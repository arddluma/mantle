package metrics

import (
	"github.com/mantlenetworkio/mantle/mt-node/eth"
	opmetrics "github.com/mantlenetworkio/mantle/mt-service/metrics"
	txmetrics "github.com/mantlenetworkio/mantle/mt-service/txmgr/metrics"
)

type noopMetrics struct {
	opmetrics.NoopRefMetrics
	txmetrics.NoopTxMetrics
}

var NoopMetrics Metricer = new(noopMetrics)

func (*noopMetrics) RecordInfo(version string) {}
func (*noopMetrics) RecordUp()                 {}

func (*noopMetrics) RecordL2BlocksProposed(l2ref eth.L2BlockRef) {}
