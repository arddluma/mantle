/* Imports: External */
import * as dotenv from 'dotenv'
import { Bcfg } from '@mantleio/core-utils'
import Config from 'bcfg'

/* Imports: Internal */
import { L1DataTransportService } from './main/service'

type ethNetwork = 'mainnet' | 'kovan' | 'goerli'
;(async () => {
  try {
    dotenv.config()

    const config: Bcfg = new Config('data-transport-layer')
    config.load({
      env: true,
      argv: true,
    })

    const service = new L1DataTransportService({
      nodeEnv: config.str('node-env', 'development'),
      ethNetworkName: config.str('eth-network-name') as ethNetwork,
      release: `data-transport-layer@${process.env.npm_package_version}`,
      dbPath: config.str('db-path', './db'),
      port: config.uint('server-port', 7878),
      hostname: config.str('server-hostname', 'localhost'),
      confirmations: config.uint('confirmations', 35),
      l1RpcProvider: config.str('l1-rpc-endpoint'),
      l1RpcProviderUser: config.str('l1-rpc-user'),
      l1RpcProviderPassword: config.str('l1-rpc-password'),
      addressManager: config.str('address-manager'),
      pollingInterval: config.uint('polling-interval', 5000),
      daPollingInterval: config.uint('da-polling-interval', 5000),
      logsPerPollingInterval: config.uint('logs-per-polling-interval', 2000),
      fraudProofWindow: config.uint('fraud-proof-window',60*60*24*7),
      daSyncStep: config.uint('da-sync-step', 100),
      daInitBatch: config.uint('da-init-batch', 0),
      startUpdateBatchIndex: config.uint('start-update-batch-index', 0),
      endUpdateBatchIndex: config.uint('end-update-batch-index', 0),
      mantleDaUpgradeDataStoreId: config.uint(
        'mantle-da-upgrade-datastore-id',
        0
      ),
      mantleDaRequestTimeout: config.uint('mantle-da-request-timeout', 12000),

      dangerouslyCatchAllErrors: config.bool(
        'dangerously-catch-all-errors',
        false
      ),
      l2RpcProvider: config.str('l2-rpc-endpoint'),
      l2RpcProviderUser: config.str('l2-rpc-user'),
      l2RpcProviderPassword: config.str('l2-rpc-password'),
      l2ChainId: config.uint('l2-chain-id'),
      syncFromL1: config.bool('sync-from-l1', false),
      syncFromL2: config.bool('sync-from-l2', false),
      syncToDa: config.bool('sync-to-da', true),
      mtBatcherHost: config.str('mt-batcher-hostname', 'http://127.0.0.1'),
      mtBatcherFetchPort: config.uint('mt-batcher-fetch-port', 8089),
      eigenUpgradeEnable: config.bool('eigen-upgrade-enable', true),

      transactionsPerPollingInterval: config.uint(
        'transactions-per-polling-interval',
        1000
      ),
      legacySequencerCompatibility: config.bool(
        'legacy-sequencer-compatibility',
        false
      ),
      defaultBackend: config.str('default-backend', 'l1'),
      l1GasPriceBackend: config.str('l1-gas-price-backend', 'l1'),
      l1StartHeight: config.uint('l1-start-height'),
      useSentry: config.bool('use-sentry', false),
      sentryDsn: config.str('sentry-dsn'),
      sentryTraceRate: config.ufloat('sentry-trace-rate', 0.05),
    })

    const stop = async (signal) => {
      console.log(`"{"msg": "${signal} - Stopping data-transport layer"}"`)
      await service.stop()
      process.exit()
    }

    process.on('SIGTERM', stop)
    process.on('SIGINT', stop)

    await service.start()
  } catch (err) {
    console.error(
      `Well, that's that. We ran into a fatal error. Here's the dump. Goodbye!`
    )

    throw err
  }
})()
