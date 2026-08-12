# SAW — Simple Agent Wallet

Powered by [AxLabs](https://axlabs.com).

Cross-platform CLI wallet for agents that pay HTTP 402 / x402 resources.

- **EVM** `eip155:*` — `exact` (EIP-3009 + Permit2) and `batch-settlement`
- **Solana** `solana:*` — partially signed versioned tx + `feePayer`
- **Hedera** `hedera:mainnet` / `hedera:testnet` — partially signed transfer + `feePayer`

Site: human docs at `/docs/`. This file is the agent-oriented manual.

## Install

Download from https://github.com/AxLabs/simple-agent-wallet/releases

Place `saw` on `PATH`. Verify: `saw version`.

From source: `make build && make install`.

Agent skill:

```bash
npx skills add AxLabs/simple-agent-wallet --skill saw -g -y
```

## Init

```bash
saw init
saw init create --family evm|solana
saw init import --family evm|solana|hedera
saw init status
```

Secrets: stdin / prompts only — never CLI flags.
Wallet: `~/.config/saw/wallet.json` (mode 0600). Override: `SAW_CONFIG_DIR`.

## RPC

`~/.config/saw/config.env`:

```bash
SAW_RPC_8453=https://mainnet.base.org
SAW_RPC_84532=https://sepolia.base.org
SAW_SOLANA_RPC=https://api.mainnet-beta.solana.com
SAW_HEDERA_NETWORK=testnet
```

## Pay (x402)

Do not curl pay URLs and scrape `PAYMENT-REQUIRED` headers. Use saw:

```bash
saw inspect <url> [--method POST --data '...']
# confirm amount / network / asset / payTo with the user
saw pay <url> --confirm [--method POST --data '...'] \
  [--network eip155:8453] [--asset 0x…] [--asset-transfer-method eip3009|permit2] \
  [--scheme exact|batch-settlement]
```

`--confirm` is required.

Permit2 (exact EVM) if needed:

```bash
saw approve-permit2 --chain-id <N> --token 0x… --confirm
```

Batch channels: `saw channels`, `saw refund <url> --confirm`.

## Rules

- Never print private keys or mnemonics after init.
- Never invent header names; use saw output.
- Never pass PAYMENT-REQUIRED / PAYMENT-SIGNATURE as argv.
- If a family is missing credentials, run `saw init` for that family first.
