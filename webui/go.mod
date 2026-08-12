// This module exists only to exclude webui/ (a JS/TS project, including
// node_modules) from the root Go module's ./... pattern matching — some
// npm dependencies (e.g. flatted) ship incidental .go files that would
// otherwise be picked up by `go build ./...`/`go test ./...` at the repo
// root. Nothing here is ever built.
module configblender/webui/_excluded

go 1.26
