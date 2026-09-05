# CLAUDE.md

規則在 [`AGENTS.md`](AGENTS.md)，範圍與分層在
[`docs/spec/001-scope-and-layering.md`](docs/spec/001-scope-and-layering.md)。
本檔只留指標。

- 每輪開場：`git status --short`、`git log --oneline -5`。
- 每輪收尾：容器裡跑 `gofmt -l . && go vet ./... && go test ./...`，更新
  `README.md` 的現況表與 `docs/spec/001` §8 的里程碑。
- git 身分是 `wicanr2@gmail.com`（repo-local）。
