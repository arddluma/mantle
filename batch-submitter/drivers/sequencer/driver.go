package sequencer

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	kms "cloud.google.com/go/kms/apiv1"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"google.golang.org/api/option"

	"github.com/mantlenetworkio/mantle/batch-submitter/bindings/ctc"
	"github.com/mantlenetworkio/mantle/batch-submitter/bindings/da"
	bsscore "github.com/mantlenetworkio/mantle/bss-core"
	"github.com/mantlenetworkio/mantle/bss-core/drivers"
	"github.com/mantlenetworkio/mantle/bss-core/metrics"
	"github.com/mantlenetworkio/mantle/bss-core/txmgr"
	l2ethclient "github.com/mantlenetworkio/mantle/l2geth/ethclient"
)

const (
	appendSequencerBatchMethodName = "appendSequencerBatch"
)

var bigOne = new(big.Int).SetUint64(1)

type Config struct {
	Name                  string
	L1Client              *ethclient.Client
	L2Client              *l2ethclient.Client
	BlockOffset           uint64
	MinTxSize             uint64
	MaxTxSize             uint64
	MaxPlaintextBatchSize uint64
	CTCAddr               common.Address
	DaUpgradeBlock        uint64
	DAAddr                common.Address
	ChainID               *big.Int
	PrivKey               *ecdsa.PrivateKey
	EnableSequencerHsm    bool
	SequencerHsmAddress   string
	SequencerHsmAPIName   string
	SequencerHsmCreden    string
	BatchType             BatchType
}

type Driver struct {
	cfg              Config
	ctcContract      *ctc.CanonicalTransactionChain
	daContract       *da.BVMEigenDataLayrChain
	rawCtcContract   *bind.BoundContract
	rawDaCtcContract *bind.BoundContract
	walletAddr       common.Address
	ctcABI           *abi.ABI
	DaABI            *abi.ABI
	metrics          *Metrics
}

func NewDriver(cfg Config) (*Driver, error) {
	ctcContract, err := ctc.NewCanonicalTransactionChain(
		cfg.CTCAddr, cfg.L1Client,
	)
	if err != nil {
		log.Error("NewCanonicalTransactionChain in error", "error", err)
		return nil, err
	}

	parsed, err := abi.JSON(strings.NewReader(
		ctc.CanonicalTransactionChainABI,
	))
	if err != nil {
		log.Error("Parse CanonicalTransactionChain in error", "error", err)
		return nil, err
	}

	ctcABI, err := ctc.CanonicalTransactionChainMetaData.GetAbi()
	if err != nil {
		log.Error("Get CanonicalTransactionChain ABI in error", "error", err)
		return nil, err
	}

	rawCtcContract := bind.NewBoundContract(
		cfg.CTCAddr, parsed, cfg.L1Client, cfg.L1Client,
		cfg.L1Client,
	)

	daContract, err := da.NewBVMEigenDataLayrChain(
		cfg.DAAddr, cfg.L1Client,
	)
	if err != nil {
		log.Error("NewBVMEigenDataLayrChain in error", "error", err)
		return nil, err
	}
	daParsed, err := abi.JSON(strings.NewReader(
		da.BVMEigenDataLayrChainABI,
	))
	if err != nil {
		log.Error("Parse BVMEigenDataLayrChain in error", "error", err)
		return nil, err
	}

	daABI, err := da.BVMEigenDataLayrChainMetaData.GetAbi()
	if err != nil {
		log.Error("Get BVMEigenDataLayrChain ABI in error", "error", err)
		return nil, err
	}

	rawDaCtcContract := bind.NewBoundContract(
		cfg.DAAddr, daParsed, cfg.L1Client, cfg.L1Client,
		cfg.L1Client,
	)

	var walletAddr common.Address
	if cfg.EnableSequencerHsm {
		walletAddr = common.HexToAddress(cfg.SequencerHsmAddress)
		log.Info("use sequencer hsm", "walletaddr", walletAddr)
	} else {
		walletAddr = crypto.PubkeyToAddress(cfg.PrivKey.PublicKey)
		log.Info("not use sequencer hsm", "walletaddr", walletAddr)
	}

	return &Driver{
		cfg:              cfg,
		ctcContract:      ctcContract,
		daContract:       daContract,
		rawCtcContract:   rawCtcContract,
		rawDaCtcContract: rawDaCtcContract,
		walletAddr:       walletAddr,
		ctcABI:           ctcABI,
		DaABI:            daABI,
		metrics:          NewMetrics(cfg.Name),
	}, nil
}

// Name is an identifier used to prefix logs for a particular service.
func (d *Driver) Name() string {
	return d.cfg.Name
}

// WalletAddr is the wallet address used to pay for batch transaction fees.
func (d *Driver) WalletAddr() common.Address {
	return d.walletAddr
}

// Metrics returns the subservice telemetry object.
func (d *Driver) Metrics() metrics.Metrics {
	return d.metrics
}

// ClearPendingTx a publishes a transaction at the next available nonce in order
// to clear any transactions in the mempool left over from a prior running
// instance of the batch submitter.
func (d *Driver) ClearPendingTx(
	ctx context.Context,
	txMgr txmgr.TxManager,
	l1Client *ethclient.Client,
) error {

	return drivers.ClearPendingTx(
		d.cfg.Name, ctx, txMgr, l1Client, d.walletAddr, d.cfg.PrivKey,
		d.cfg.ChainID,
	)
}

// GetBatchBlockRange returns the start and end L2 block heights that need to be
// processed. Note that the end value is *exclusive*, therefore if the returned
// values are identical nothing needs to be processed.
func (d *Driver) GetBatchBlockRange(
	ctx context.Context) (*big.Int, *big.Int, error) {

	blockOffset := new(big.Int).SetUint64(d.cfg.BlockOffset)

	start, err := d.ctcContract.GetTotalElements(&bind.CallOpts{
		Pending: false,
		Context: ctx,
	})
	if err != nil {
		return nil, nil, err
	}
	start.Add(start, blockOffset)

	latestHeader, err := d.cfg.L2Client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	var end *big.Int
	// for upgrade
	if latestHeader.Number.Cmp(big.NewInt(int64(d.cfg.DaUpgradeBlock))) < 0 {
		// Add one because end is *exclusive*.
		end = new(big.Int).Add(latestHeader.Number, bigOne)
	} else {
		end, err = d.daContract.GetL2ConfirmedBlockNumber(&bind.CallOpts{
			Context: context.Background(),
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if start.Cmp(end) > 0 {
		return nil, nil, fmt.Errorf("invalid range, "+
			"end(%v) < start(%v)", end, start)
	}
	return start, end, nil
}

// CraftBatchTx transforms the L2 blocks between start and end into a batch
// transaction using the given nonce. A dummy gas price is used in the resulting
// transaction to use for size estimation. A nil transaction is returned if the
// transaction does not meet the minimum size requirements.
//
// NOTE: This method SHOULD NOT publish the resulting transaction.
func (d *Driver) CraftBatchTx(
	ctx context.Context,
	start, end, nonce *big.Int,
) (*types.Transaction, error) {

	name := d.cfg.Name

	log.Info(name+" crafting batch tx", "start", start, "end", end,
		"nonce", nonce, "type", d.cfg.BatchType.String())

	var (
		batchElements  []BatchElement
		totalTxSize    uint64
		hasLargeNextTx bool
	)
	for i := new(big.Int).Set(start); i.Cmp(end) < 0; i.Add(i, bigOne) {
		block, err := d.cfg.L2Client.BlockByNumber(ctx, i)
		if err != nil {
			return nil, err
		}

		// For each sequencer transaction, update our running total with the
		// size of the transaction.
		batchElement := BatchElementFromBlock(block)
		if batchElement.IsSequencerTx() {
			// Abort once the total size estimate is greater than the maximum
			// configured size. This is a conservative estimate, as the total
			// calldata size will be greater when batch contexts are included.
			// Below this set will be further whittled until the raw call data
			// size also adheres to this constraint.
			txLen := batchElement.Tx.Size()
			if totalTxSize+uint64(TxLenSize+txLen) > d.cfg.MaxPlaintextBatchSize {
				// Adding this transaction causes the batch to be too large, but
				// we also record if the batch size without the transaction
				// fails to meet our minimum size constraint. This is used below
				// to determine whether or not to ignore the minimum size check,
				// since in this case it can't be avoided.
				hasLargeNextTx = totalTxSize < d.cfg.MinTxSize
				break
			}
			totalTxSize += uint64(TxLenSize + txLen)
		}

		batchElements = append(batchElements, batchElement)
	}

	shouldStartAt := start.Uint64()
	var pruneCount int
	for {
		batchParams, err := GenSequencerBatchParams(
			shouldStartAt, d.cfg.BlockOffset, batchElements,
		)
		if err != nil {
			return nil, err
		}

		// Encode the batch arguments using the configured encoding type.
		batchArguments, err := batchParams.Serialize(d.cfg.BatchType, start, big.NewInt(int64(d.cfg.DaUpgradeBlock)))
		if err != nil {
			return nil, err
		}

		appendSequencerBatchID := d.ctcABI.Methods[appendSequencerBatchMethodName].ID
		calldata := append(appendSequencerBatchID, batchArguments...)

		log.Info(name+" testing batch size",
			"calldata_size", len(calldata),
			"min_tx_size", d.cfg.MinTxSize,
			"max_tx_size", d.cfg.MaxTxSize)

		// Continue pruning until plaintext calldata size is less than
		// configured max.
		calldataSize := uint64(len(calldata))
		if calldataSize > d.cfg.MaxTxSize {
			oldLen := len(batchElements)
			newBatchElementsLen := (oldLen * 9) / 10
			batchElements = batchElements[:newBatchElementsLen]
			log.Info(name+" pruned batch",
				"old_num_txs", oldLen,
				"new_num_txs", newBatchElementsLen)
			pruneCount++
			continue
		}

		// There are two specific cases in which we choose to ignore the minimum
		// L1 tx size. These cases are permitted since they arise from
		// situations where the difference between the configured MinTxSize and
		// MaxTxSize is less than the maximum L2 tx size permitted by the
		// mempool.
		//
		// This configuration is useful when trying to ensure the profitability
		// is sufficient, and we permit batches to be submitted with less than
		// our desired configuration only if it is not possible to construct a
		// batch within the given parameters.
		//
		// The two cases are:
		// 1. When the next elenent is larger than the difference between the
		//    min and the max, causing the batch to be too small without the
		//    element, and too large with it.
		// 2. When pruning a batch that exceeds the mac size below, and then
		//    becomes too small as a result. This is avoided by only applying
		//    the min size check when the pruneCount is zero.
		ignoreMinTxSize := pruneCount > 0 || hasLargeNextTx
		if !ignoreMinTxSize && calldataSize < d.cfg.MinTxSize {
			log.Info(name+" batch tx size below minimum",
				"num_txs", len(batchElements))
			return nil, nil
		}

		d.metrics.NumElementsPerBatch().Observe(float64(len(batchElements)))
		d.metrics.BatchPruneCount.Set(float64(pruneCount))

		log.Info(name+" batch constructed",
			"num_txs", len(batchElements),
			"final_size", len(calldata),
			"batch_type", d.cfg.BatchType)

		var opts *bind.TransactOpts
		if d.cfg.EnableSequencerHsm {
			seqBytes, err := hex.DecodeString(d.cfg.SequencerHsmCreden)
			apikey := option.WithCredentialsJSON(seqBytes)
			client, err := kms.NewKeyManagementClient(ctx, apikey)
			if err != nil {
				log.Info("sequencer", "create signer error", err.Error())
				return nil, err
			}
			mk := &bsscore.ManagedKey{
				KeyName:      d.cfg.SequencerHsmAPIName,
				EthereumAddr: common.HexToAddress(d.cfg.SequencerHsmAddress),
				Gclient:      client,
			}
			opts, err = mk.NewEthereumTransactorrWithChainID(ctx, d.cfg.ChainID)
			if err != nil {
				log.Info("sequencer", "create signer error", err.Error())
				return nil, err
			}
		} else {
			opts, err = bind.NewKeyedTransactorWithChainID(
				d.cfg.PrivKey, d.cfg.ChainID,
			)
			if err != nil {
				log.Info("sequencer", "create signer error", err.Error())
				return nil, err
			}
		}
		if err != nil {
			return nil, err
		}
		opts.Context = ctx
		opts.Nonce = nonce
		opts.NoSend = true

		tx, err := d.rawCtcContract.RawTransact(opts, calldata)
		switch {
		case err == nil:
			return tx, nil

		// If the transaction failed because the backend does not support
		// eth_maxPriorityFeePerGas, fallback to using the default constant.
		// Currently Alchemy is the only backend provider that exposes this
		// method, so in the event their API is unreachable we can fallback to a
		// degraded mode of operation. This also applies to our test
		// environments, as hardhat doesn't support the query either.
		case drivers.IsMaxPriorityFeePerGasNotFoundError(err):
			log.Warn(d.cfg.Name + " eth_maxPriorityFeePerGas is unsupported " +
				"by current backend, using fallback gasTipCap")
			opts.GasTipCap = drivers.FallbackGasTipCap
			return d.rawCtcContract.RawTransact(opts, calldata)

		default:
			return nil, err
		}
	}
}

// UpdateGasPrice signs an otherwise identical txn to the one provided but with
// updated gas prices sampled from the existing network conditions.
//
// NOTE: Thie method SHOULD NOT publish the resulting transaction.
func (d *Driver) UpdateGasPrice(
	ctx context.Context,
	tx *types.Transaction,
) (*types.Transaction, error) {

	gasTipCap, err := d.cfg.L1Client.SuggestGasTipCap(ctx)
	if err != nil {
		// If the transaction failed because the backend does not support
		// eth_maxPriorityFeePerGas, fallback to using the default constant.
		// Currently Alchemy is the only backend provider that exposes this
		// method, so in the event their API is unreachable we can fallback to a
		// degraded mode of operation. This also applies to our test
		// environments, as hardhat doesn't support the query either.
		if !drivers.IsMaxPriorityFeePerGasNotFoundError(err) {
			return nil, err
		}

		log.Warn(d.cfg.Name + " eth_maxPriorityFeePerGas is unsupported " +
			"by current backend, using fallback gasTipCap")
		gasTipCap = drivers.FallbackGasTipCap
	}

	header, err := d.cfg.L1Client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	gasFeeCap := txmgr.CalcGasFeeCap(header.BaseFee, gasTipCap)

	// The estimated gas limits performed by RawTransact fail semi-regularly
	// with out of gas exceptions. To remedy this we extract the internal calls
	// to perform gas price/gas limit estimation here and add a buffer to
	// account for any network variability.
	gasLimit, err := d.cfg.L1Client.EstimateGas(ctx, ethereum.CallMsg{
		From:      d.walletAddr,
		To:        &d.cfg.CTCAddr,
		GasPrice:  nil,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Value:     nil,
		Data:      tx.Data(),
	})
	if err != nil {
		return nil, err
	}

	var opts *bind.TransactOpts
	if d.cfg.EnableSequencerHsm {
		seqBytes, err := hex.DecodeString(d.cfg.SequencerHsmCreden)
		apikey := option.WithCredentialsJSON(seqBytes)
		client, err := kms.NewKeyManagementClient(ctx, apikey)
		if err != nil {
			return nil, err
		}
		mk := &bsscore.ManagedKey{
			KeyName:      d.cfg.SequencerHsmAPIName,
			EthereumAddr: common.HexToAddress(d.cfg.SequencerHsmAddress),
			Gclient:      client,
		}
		opts, err = mk.NewEthereumTransactorrWithChainID(ctx, d.cfg.ChainID)
		log.Info("sequencer", "enable-hsm", true)
	} else {
		opts, err = bind.NewKeyedTransactorWithChainID(
			d.cfg.PrivKey, d.cfg.ChainID,
		)
	}
	if err != nil {
		return nil, err
	}
	opts.Context = ctx
	opts.Nonce = new(big.Int).SetUint64(tx.Nonce())
	opts.GasTipCap = gasTipCap
	opts.GasFeeCap = gasFeeCap
	opts.GasLimit = 6 * gasLimit / 5 // add 20% buffer to gas limit
	opts.NoSend = true

	return d.rawCtcContract.RawTransact(opts, tx.Data())
}

// SendTransaction injects a signed transaction into the pending pool for
// execution.
func (d *Driver) SendTransaction(
	ctx context.Context,
	tx *types.Transaction,
) error {
	return d.cfg.L1Client.SendTransaction(ctx, tx)
}
