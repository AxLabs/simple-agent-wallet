# saw — Simple Agent Wallet

Cross-platform Go CLI for multi-family wallets and x402 `exact` payments.

Supports **EVM** (`eip155:*` — `exact` EIP-3009/Permit2 and **`batch-settlement`**), **Solana** (`solana:*`), and **Hedera** (`hedera:mainnet` / `hedera:testnet`).

## Install

Download a release binary from the [latest release](https://github.com/AxLabs/simple-agent-wallet/releases/latest), or build from source:

```bash
make build     # → ./bin/saw
make install   # → ~/.local/bin/saw (requires ~/.local/bin on PATH)
```

Agent skill:

```bash
npx skills add AxLabs/simple-agent-wallet --skill saw -g -y
```

## Quick start

```bash
saw init                    # interactive wizard (add EVM / Solana / Hedera)
saw init status
saw address
saw inspect https://example.com/paid
saw pay https://example.com/paid --confirm
saw pay https://example.com/paid --confirm --skip-balance-check   # sign without balance preflight
```

Secrets are never accepted via CLI flags — only stdin / interactive prompts. Wallet file: `~/.config/saw/wallet.json` (mode `0600`). RPC endpoints: `~/.config/saw/config.env`.

## Commands

| Command | Purpose |
|---------|---------|
| `saw init` / `create` / `import` / `status` | Multi-family wallet lifecycle |
| `saw address [--family]` | Print configured addresses |
| `saw balance …` | Native / token balances |
| `saw inspect <url>` | Decode 402 `accepts[]` |
| `saw pay <url> --confirm` | Sign and retry (`exact` or `batch-settlement`); checks selected token balance first (`--skip-balance-check` to opt out) |
| `saw channels` / `saw refund <url> --confirm` | Batch-settlement sessions / cooperative refund |
| `saw approve-permit2 --chain-id N --token 0x… --confirm` | ERC-20 approve Permit2 |
| `saw transfer … --confirm` | EVM / Solana / Hedera transfers |
| `saw call` / `saw abi encode` | EVM only |

## Agent skill

See [`skill/saw/SKILL.md`](skill/saw/SKILL.md).

## Website

Static site in [`docs/`](docs/) (GitHub Pages). Local preview:

```bash
make docs-serve
# → http://127.0.0.1:4173/
```

## Development

```bash
make test
make build
```

Requires local `../x402/go` (go.mod `replace`) — use the AxLabs `x402` fork while Hedera mechanisms are not upstream. CI checks out `AxLabs/x402@feat/go-mechanisms-hedera-exact`.

## Releasing

Push a `v*` tag. Ubuntu GoReleaser publishes Linux and Windows. The org Mac Mini self-hosted runner (`self-hosted`, `macOS`, `X64`) then builds, Developer ID-signs, and notarizes `darwin/amd64` and `darwin/arm64` and attaches them to the same GitHub Release. Notarization is sequential per arch and can take over an hour each; the `sign-macos` job is allowed 6 hours so GitHub does not cancel a live `notarytool wait`.

The Mini needs Xcode CLT (`notarytool`) and `gh` on `PATH`. Signing credentials are repo (or org) Actions secrets — not files in this repository. Required: `MACOS_KEYCHAIN_PASSWORD`, `MACOS_KEYCHAIN_PATH`, `MACOS_SIGN_IDENTITY`, `MACOS_NOTARY_KEY_ID`, `MACOS_NOTARY_ISSUER_ID`, `MACOS_NOTARY_KEY_PATH`.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
