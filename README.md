# A Quant Simulator[🔨Developing]

Welcome to this Repo! This is a local-first quantitative research, backtesting, and paper trading platform.

FinanceSimulator lets you develop Python trading strategies, validate them with historical market data, and run them through a paper-trading environment powered by the same matching and portfolio engine.

The project is designed for private, local use: strategy code, market data, backtest parameters and trading records remain on your own machine.

Thanks a lot for Ashley's Mental and Coffee Support!

## Why Quant Simulator?

- Keep your strategy code and research data local
- Use one matching and portfolio engine for both backtesting and paper trading
- Support replaceable market-data providers
- Treat corporate actions, transaction costs, and portfolio accounting as first-class concerns

## Project Status

> This is an actively developed personal project. The architecture and core domain modules are under construction.

| Area | Status |
| --- | --- |
| Yahoo Finance data source | Done |
| Portfolio and ledger engine | Done |
| Parquet market-data storage | Done |
| SQLite metadata storage | Done |
| Event-driven backtesting | Planned |
| Sandboxed Python strategy worker | Planned |
| React frontend | Planned |
| Paper trading | Planned |

## Architecture

- **Backend**: Go
- **Historical OHLCV data**: Parquet
- **Business data, ledgers, and backtest metadata:** PostgreSQL
- **Frontend**: React
- **Communication:** RPC between the Go backend and Python worker

## Disclaimer
This project is for educational and personal research purposes only. It does not provide investment advice and should not be used for live trading without independent validation.