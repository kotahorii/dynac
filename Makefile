.PHONY: run modernize

run:
	go run ./cmd/dynac

modernize:
	go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@latest -fix ./...
