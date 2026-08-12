---
name: saw
description: >-
  Simple Agent Wallet (saw) for x402 exact and batch-settlement payments across
  EVM, Solana, and Hedera. Use when an HTTP response is 402 Payment Required,
  the user needs to inspect/pay an x402 resource, approve Permit2, manage
  batch-settlement channels/refunds, transfer tokens, or manage a multi-family
  agent wallet. Install saw from GitHub Releases; never put keys in argv.
---

# saw — Simple Agent Wallet

Cross-platform CLI for agent wallets and x402 payments.

- **EVM** `eip155:*` — `exact` (EIP-3009 + Permit2) and **`batch-settlement`** (channel deposits + vouchers)
- **Solana** `solana:*` — partially signed versioned tx + `feePayer`
- **Hedera** `hedera:mainnet` / `hedera:testnet` — partially signed transfer + `feePayer`

## Install

Download the binary for the host OS/arch from:

https://github.com/AxLabs/simple-agent-wallet/releases/latest

Place `saw` on `PATH`. Verify: `saw version`.

From source (monorepo): `make build && make install`.

## Init

```bash
saw init                              # interactive TTY wizard
saw init create --family evm
saw init create --family solana
saw init import --family evm          # private key or mnemonic on stdin
saw init import --family solana
saw init import --family hedera --account-id 0.0.x   # key on stdin
saw init status                       # addresses only; never secrets
```

Secrets: **stdin / prompts only** — never CLI flags. Optional encryption via `--encrypt` / `SAW_PASSWORD`.

Wallet: `~/.config/saw/wallet.json` (mode `0600`). Override dir: `SAW_CONFIG_DIR`.

## RPC config

Edit `~/.config/saw/config.env` (see `skill/saw/config.example`):

```bash
SAW_RPC_8453=https://mainnet.base.org
SAW_RPC_84532=https://sepolia.base.org
SAW_SOLANA_RPC=https://api.mainnet-beta.solana.com
SAW_HEDERA_NETWORK=testnet
```

## Pay flow (generic x402)

**Do not** `curl` the pay URL and scrape `PAYMENT-REQUIRED` / `payment-required` headers.
Agent tool output truncates long base64 headers. `saw` fetches and signs them itself.

1. Inspect options (do not pay yet):

```bash
saw inspect <url> [--method GET|POST] [--data '...']
```

Read `accepts[]`: `scheme` (`exact` | `batch-settlement`), `family`, `network`, `asset`, `amount`, `payTo`, `assetTransferMethod` (exact EVM), `feePayer` (Solana/Hedera).

2. Confirm with the user: amount, asset, network, payTo, scheme, and feePayer when present.

3. If EVM **permit2** (exact) and allowance may be missing:

```bash
saw approve-permit2 --chain-id <N> --token 0x… --confirm
```

4. Pay once (same method/body as inspect):

```bash
saw pay <url> --confirm \
  [--scheme exact|batch-settlement] \
  [--network eip155:8453|solana:…|hedera:testnet] \
  [--asset 0x…] \
  [--asset-transfer-method eip3009|permit2] \
  [--method POST --data '...'] \
  [--order-status 'https://…/status?tx={tx}']
```

For **batch-settlement**, configure `SAW_RPC_<chainId>` so channel recovery works. Sessions persist under `~/.config/saw/channels/`. Optional deposit sizing: `SAW_BATCH_DEPOSIT_MULTIPLIER` (default 5).

```bash
saw channels                          # list local channel sessions (no secrets)
saw refund <url> --confirm            # cooperative channel refund
saw refund <url> --amount <units> --confirm
```

`--confirm` is required. One attempt; no loops without user confirmation.

5. Transfers / EVM extras when needed:

```bash
saw transfer --family evm --chain-id N --to 0x… --amount <wei> --confirm
saw transfer --family evm --chain-id N --token 0x… --to 0x… --amount <units> --confirm
saw transfer --family solana --to <pubkey> --amount <lamports> --confirm
saw transfer --family hedera --to 0.0.x --amount <tinybars> --confirm
saw call --chain-id N --to 0x… --data 0x… --confirm
saw abi encode 'transfer(address,uint256)' 0x… 1000
```

## Rules

- Never print private keys or mnemonics to the user-facing chat after init.
- Never invent header names — use `saw inspect` / `saw pay` output.
- Never pass a `PAYMENT-REQUIRED` / `PAYMENT-SIGNATURE` token as a CLI argument; `saw` handles headers.
- Discovery of merchant URLs comes from the user or the 402 body — this skill is not vendor-specific.
- If the selected accept family has no wallet credentials, run `saw init` for that family first.
