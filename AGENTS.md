# 作業鐵則

## 環境

- **Docker-only**：Go 的建置與測試都在容器裡跑，主機只做 `git` 與檔案編輯。

  ```sh
  docker run --rm --network none --memory 2g --cpus 2 \
    -e GOPATH=/tmp/gp -e GOCACHE=/tmp/gc -e GOPROXY=off -e GOFLAGS=-buildvcs=false \
    -v "$PWD:/src" -w /src dsds-go:1.25 \
    sh -c 'export PATH=/usr/local/go/bin:$PATH; gofmt -l . && go vet ./... && go test ./...'
  ```

- 一次性工作一律 `--rm`，帶 `--memory`／`--cpus`／`--pids-limit`，預設
  `--network none`。禁止任何 `prune`／`rmi`／動別的專案的容器。

## 原版資料不進版控

原版執行檔、`.RSC`、存檔、擷取幀都**不提交**（`.gitignore` 擋了 `*.EXE`）。
測試用**合成映像**（見 `internal/ne/ne_test.go` 的 `buildNE`）；對真檔的驗證
用 `cmd/neinfo` 手動跑，數字記進 `docs/spec/`。

## 先量再寫

決定「要不要實作某個東西」之前先量。API 表面用 `neinfo` 量、熱點用引用次數
量。`docs/spec/001` §3 的「不做 `AllocSelector`」是量過匯入表才決定的——
**寫明「量過才不做」與「忘了做」的差別**，因為後面的人分不出來。

## 兩份獨立實作才算證實

`neinfo` 對 CIV.EXE 的數字是與一支獨立的 Python 解析器對過的。新增任何
「原版是這樣」的斷言時，想一下第二個來源在哪裡。

## 斷言會過期

「某某未實作」「某某語意未知」這種句子宣稱的是**目前世界的狀態**，做完之後
沒有人會回頭改。寫這種句子時：

- 說得出它**什麼時候會不成立**；
- 解掉之後**回頭 grep 那個符號名**，把所有抄過它的地方一起改。

（這條是從 civ1 專案帶過來的教訓：一組四支函式的「未解清單」被十三份文件
各抄一次，四支解掉之後沒有一份回頭改。）
