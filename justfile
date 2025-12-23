# 開発サーバー起動（Vite + Go）
dev:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    cd frontend && pnpm run dev &
    VITE_DEV=true go tool air

# Goサーバーのみ（Viteは別ターミナルで起動）
dev-go:
    VITE_DEV=true go tool air

# Viteのみ
dev-vite:
    cd frontend && pnpm run dev

# 本番ビルド
build:
    cd frontend && pnpm run build
    go build .

# テンプレート生成
templ:
    go tool templ generate

# マイグレーション実行
migrate-up:
    go tool goose -dir db/migrations sqlite3 db/app.db up

migrate-down:
    go tool goose -dir db/migrations sqlite3 db/app.db down

# bobモデル生成
bobgen:
    go tool bobgen-sqlite
