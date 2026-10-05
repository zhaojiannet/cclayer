# cclayer チュートリアル

[English](tutorial.en.md) · [中文](tutorial.zh.md)

このチュートリアルは、cclayer が何か、どう入れるか、どう使うかをゼロから説明する。README を読んでもはっきりしないところは、ここを読む。

## 1. 何を解決するか

Claude Code の設定はすべて `~/.claude/` の下にある：グローバルなルール `CLAUDE.md`、`rules/`、`skills/`、出力スタイル、hook スクリプト、`settings.json`、プラグイン、MCP サーバー。これらは一度直したら、どのマシンでも同じであってほしい。

一方、手元のプロジェクトは 1 つのチームから来るとは限らない。各チームには自分の git コミットの身元、専用のルールと hook があり、それらはそのチームのプロジェクトでだけ有効になるべきで、別のプロジェクトに混ざってはならず、別のチームのマシンに現れてもならない。

cclayer は設定を 2 種類の「レイヤー」に分ける：

| レイヤー | 入れるもの | 数 | 公開できるか |
|---|---|---|---|
| ベースレイヤー `base` | すべてのマシンで同じ部分：`CLAUDE.md`、`rules/`、`skills/`、出力スタイル、hook スクリプト、`settings.json` の共有キー、プラグインとマーケットプレイスの一覧、MCP 定義 | 1 つ | できる。身元情報を一切含まない |
| オーバーレイ | 1 つのチーム専用の部分：git の身元、hook、そのリポジトリのリモート URL パターン、そのプロジェクトに書く `.claude/settings.local.json` のキーと `CLAUDE.local.md` のブロック | チームごとに 1 つ | 非公開 |

各マシンはベースレイヤーと、使うことを許されたオーバーレイだけを取り込む。1 コマンドでこのマシンに適用し、1 コマンドでこのマシンの変更を書き戻す。

典型的な使い方は 3 つ：

- **自分だけ、マシンが数台**：ベースレイヤー 1 つで足りる。オーバーレイはなくてよい。
- **複数のチーム、自分のマシン**：ベースレイヤーにチームごとのオーバーレイを加え、プロジェクトのリモート URL に応じて自動で適用する。
- **チームから支給されたマシン**：ベースレイヤーにそのチーム 1 つのオーバーレイを加え、他のチームのレイヤーは入れない。

## 2. いくつかの用語

- **レイヤー**：ルートに `layer.toml` を持つディレクトリ。git リポジトリ（cclayer がこのマシンに clone する）でもよいし、このマシン上の 1 つのディレクトリ、たとえばクラウドドライブの同期フォルダ内のディレクトリでもよい。
- **デバイスマニフェスト**：`~/.config/cclayer/device.toml`。このマシンで有効なレイヤー、レイヤーのこのマシン上の場所、プロジェクトを探す場所を記録する。このマシンにだけあり、どのリポジトリにも入らない。
- **clone ディレクトリ**：git レイヤーのこのマシン上の複製。既定は `~/.local/share/cclayer/<layer>`。setup の「ローカルの clone 先」の行か、デバイスマニフェストの `clone_dir` で親フォルダを変えられる。ディレクトリレイヤーには複製がなく、そのディレクトリ自体がレイヤー。
- **プロジェクトルート（roots）**：cclayer はこれらのディレクトリの下で git リポジトリを探し、そのリモート URL がどのオーバーレイに属するかを見る。
- **一致**：オーバーレイの `[[match]] remote = "github.com/acme-inc/*"` があるリポジトリのリモート URL に合えば、そのリポジトリはこのオーバーレイに属する。1 つのリポジトリが属するオーバーレイは最大 1 つ。
- **注入**：apply がオーバーレイの `project/` 以下の内容を、一致したプロジェクトの `.claude/settings.local.json` と `CLAUDE.local.md` に書く。この 2 つのファイルはグローバルな git の除外リストに登録され、プロジェクトにコミットされない。
- **状態ディレクトリ**：`~/.local/state/cclayer/`。前回の apply が書いたファイルのハッシュ、プロジェクトの所属、実行済みのプラグインコマンド、上書きしたファイルのバックアップを記録する。

cclayer が触らないもの：Claude Code のログイン、セッション記録、プロンプト履歴、`~/.claude.json`、`projects/`、そして `settings.json` の `env`、`permissions.allow`、`permissions.defaultMode`、`permissions.ask`。これらは常にこのマシンに残る。

## 3. インストール

macOS では Homebrew を使う：

```sh
brew install --cask zhaojiannet/tap/cclayer
```

新しいバージョンが出たら `brew upgrade --cask cclayer` で更新する。

Linux、Windows、Homebrew を使わないマシンでは、[GitHub Releases](https://github.com/zhaojiannet/cclayer/releases) から該当プラットフォームのアーカイブをダウンロードし、展開して `cclayer` を PATH に置く。

git も必要。プラグイン、MCP、`leave` の purge の手順には Claude Code 2.1.288 以降が必要。PATH に `claude` がなければ、プラグインと MCP の手順はスキップされてその旨が表示され、`leave` は削除の前に止まる。

インストール後：

```sh
cclayer version
cclayer help
```

## 4. 最も簡単な使い方：1 人、数台のマシン、ディレクトリ同期

git に触りたくなければ、レイヤーは 1 つのディレクトリでよい。クラウドドライブの同期フォルダに置き、各マシンで同じパスを入れる。

### 1 台目のマシン

```sh
cclayer setup
```

setup は全画面の編集画面。左の列の上は「レイヤー」で、このマシンが使うベースレイヤーとオーバーレイを並べる。下は「このマシン」で、プロジェクトのディレクトリや自動 pull など、マシンに属する設定を置く。右の列には左で選んだ項目の説明と編集できる欄があり、保存すると何が起きるか（どこに clone するか、どこに初期ファイルを書くか）も表示する。最下行には保存に足りないものが表示され、その横に「保存して適用」「保存のみ」「終了」の 3 つのボタンがある。開いた時点でカーソルは最初に入力すべき欄にある。上下キーで選び、Enter で編集し、Tab で左の列、右の列、ボタンの間を移動し、Esc で戻る。`?` ですべてのキー操作を表示する。保存するまでは何も書き込まない。表示言語はシステムに従い、「言語」の行で English、简体中文、日本語を切り替えられる。

このマシンで変えるのは 2 行だけ：

1. **ベースレイヤー**：ディレクトリを入れる。たとえば `~/Dropbox/cclayer/base`。ディレクトリがまだなければ、保存時に初期の `layer.toml` を書き込む。
2. **プロジェクト**：コードを置くディレクトリをカンマ区切りで。たとえば `~/Projects`。

ほかの行は既定のままでよい。自動 pull は既定でオフ。オンにすると、第 10 節の SessionStart hook をベースレイヤーの `claude/settings.json` に入れたあと、Claude Code のセッションを開くたびに先に自動で `apply` する（ディレクトリレイヤーには pull の手順がなく、apply だけ行う）。profiles はまずオフのまま（第 9 節で扱う）。信頼するレイヤーは選ばない。最後に「保存して適用」を選ぶ。apply のあと `doctor` が自動で実行される。

初期の `layer.toml` はこうなる：

```toml
[layer]
name = "base"
kind = "base"
# private = true   # only you use this layer and it lives somewhere private:
#                  # check then lets email addresses through

[claude]
paths = ["CLAUDE.md", "rules/", "skills/", "output-styles/", "agents/", "hooks/"]
settings_keys = ["attribution", "permissions.deny", "hooks", "statusLine",
  "enabledPlugins", "extraKnownMarketplaces", "outputStyle", "effortLevel", "theme"]
ignore = ["skills/.trash/"]
```

`paths` はレイヤーが管理する `~/.claude` 以下のファイルとディレクトリ、`settings_keys` はレイヤーが管理する `settings.json` のキー。自分だけが使い、ディレクトリも自分のクラウドドライブにしかないなら、`private = true` の行のコメントを外す。`check` はルールに書いたメールアドレスを拒否しなくなる。この時点ではレイヤーはまだ空なので、apply は何も書かない。

次に、このマシンにある既存の設定をレイヤーに取り込む：

```sh
cclayer capture
```

「このマシンにだけあり、レイヤーにはない」ファイルを一覧し、`--add` で取り込むよう案内する：

```sh
cclayer capture --add CLAUDE.md --add rules/ --add skills/
```

パスは `~/.claude` からの相対。書く前に各ファイルが `check` を通る：秘密情報の形をした文字列、メールアドレス、ホームディレクトリを指す絶対パスなどは拒否され、どの行かが示されるので、直してから取り込む。`settings.json` には `--add` は不要で、`settings_keys` に挙げたキーはこのマシンで変更があれば書き戻される。

それから確認する：

```sh
cclayer status
ls ~/Dropbox/cclayer/base/claude
```

クラウドドライブがディレクトリを同期し終えるのを待つ。

### 2 台目のマシン

cclayer を入れ、クラウドドライブが `~/Dropbox/cclayer/base` を同期し終えるのを待ってから：

```sh
cclayer setup
```

ベースレイヤーの場所には同じパスを入れる。今回はディレクトリに `layer.toml` が既にあるので、setup はそれをそのまま使う。「保存して適用」を選ぶと、このマシンの `~/.claude` に 1 台目のマシンの設定が入る。このマシンに元からあり内容が違う同名のファイルは競合として扱われる。apply が表示して上書きするか尋ね、同意すれば古い版を先に `~/.local/state/cclayer/backups/` にバックアップしてから上書きする。

### その後

- どのマシンで `~/.claude` 以下を変えたときも：`cclayer capture` して、同期を待つ。
- 別のマシンで：`cclayer apply`。自動 pull を有効にし、第 10 節の hook を入れていれば、Claude Code のセッションを開くときに自動で行われる。
- 両方で同じファイルを変えた場合：`apply` はそれを競合として一覧し、対話的に実行していればレイヤー側の版でこのマシンを上書きするか尋ね、自動モードでは触らない。

注意：クラウドドライブが同期するのは普通のファイルで、バージョン履歴はない。2 台のマシンで同じファイルを同時に変えたときはクラウドドライブの処理に従う。履歴とマージが欲しければ、第 5 節の git の方法を使う。

## 5. git リポジトリで同期する

### ベースレイヤーのリポジトリを作る

一番手間がないのは、まず第 4 節のとおりこのマシンにディレクトリレイヤーを作って内容を取り込み、そのディレクトリをリポジトリにすることだ：

```sh
cd ~/cclayer-base            # setup で入れたディレクトリ
git init -b main
git add -A
git commit -m "base layer"
git remote add origin git@github.com:you/cclayer-base.git
git push -u origin main
```

ベースレイヤーには身元情報がないので、公開リポジトリでよい。非公開でもよい。

先に GitHub でリポジトリを作り、`layer.toml` と `claude/` ディレクトリを手で書いて push してから、マシンの `setup` でリポジトリの URL を入れてもよい。空のリポジトリは不可。`setup` はその中に `layer.toml` を見つける必要がある。

### 他のマシン

```sh
cclayer setup
```

ベースレイヤーの場所にはリポジトリの URL、`https://github.com/you/cclayer-base.git` か `git@github.com:you/cclayer-base.git` を入れる。公開リポジトリなら、アクセス方法で「なし」を選ぶ。cclayer はそれを `~/.local/share/cclayer/base` に clone する。

### 日常

```sh
cclayer push                 # アップロード：capture し、コミットする内容を示してコミットと push
cclayer apply --pull         # ダウンロード：clone を最新に fast-forward し、このマシンに適用
```

`push` はコミットの前に各レイヤーの変更を一覧し、一度だけ尋ねる。その間にほかのマシンが push していれば、このマシンのコミットをその上に載せる。相手のコミットと競合したら止まり、clone は元のまま残る。`-m` はすべてのレイヤーに同じメッセージを使う。レイヤーごとに別のメッセージを書きたければ、`cclayer capture` のあと各 clone の中で自分でコミットする。

`capture` はあえてコミットも push もしない。先に diff を見られるようにするためだ。`status` は各 clone に未コミットの変更があるか、リモートより何コミット進んでいるかを表示する。

## 6. チームを 1 つ追加する：オーバーレイ

チームごとに非公開リポジトリ（またはディレクトリ）を 1 つ。内容：

```
layer.toml
project/settings.local.json    一致したプロジェクトの .claude/settings.local.json に書くキー
project/CLAUDE.local.md        一致したプロジェクトの CLAUDE.local.md に書く管理ブロック
git/fragment.gitconfig         任意。このチームの hook と alias。一致したリポジトリでのみ有効
```

`layer.toml`：

```toml
[layer]
name = "acme"
kind = "overlay"

[identity]
name = "Full Name"
email = "me@acme.example"

[[match]]
remote = "github.com/acme-inc/*"

[inject]
settings_keys = ["permissions.deny", "enabledPlugins", "extraKnownMarketplaces"]
claude_local = "project/CLAUDE.local.md"

[git]
fragment = "git/fragment.gitconfig"
```

節ごとに説明する：

- `[identity]`：一致したリポジトリでは git がこの名前とメールアドレスでコミットする。cclayer は `includeIf "hasconfig:remote.*.url:..."` によってこれらのリポジトリでだけ有効にし、他のリポジトリには触れない。
- `[[match]]`：リモート URL のパターン。ホストと組織はワイルドカードを使わず文字どおりに書き、2 つのオーバーレイが同じホストと組織を書くことはできない。その後ろは `*` が 1 区切り、`**` が複数区切りに一致する。複数書ける。`https://`、`git@`、`ssh://` の 3 種類の書き方のリモートすべてに、大文字小文字が違っても合う。
- `[inject] settings_keys`：`project/settings.local.json` のどのキーをプロジェクトに書くか。`permissions.allow` は決して動かさず、プロジェクト側で自分が許可した項目は残る。
- `[inject] claude_local`：このファイルの内容をプロジェクトの `CLAUDE.local.md` の `<!-- cclayer:begin -->` と `<!-- cclayer:end -->` の間に書く。ブロック外の内容には触れない。
- `[git] fragment`：生の git 設定フラグメント。`[user]`（身元は `[identity]` で扱う）、リモート URL、`include` は不可。`pull.rebase`、`push.default`、`merge.conflictstyle`、`diff.algorithm` のようにキーごとに列挙したよくある設定は置ける。alias を含むそれ以外のキー、特に `core.hooksPath`、`credential.helper` など git にプログラムを実行させるものを設定するなら、このレイヤーをデバイスマニフェストの `trust_exec` に載せる必要がある。載っていなければ apply は拒否する。

オーバーレイに `[claude]` 節は置けない。オーバーレイは `~/.claude/` に何も書かない。

setup でオーバーレイを追加するとき、存在しないディレクトリを入れると、setup は続けてコミッターの名前、メールアドレス、リモートパターンを尋ね、保存時に上記の初期ファイル一式を生成する。ただし `[git]` 節とフラグメントファイルは含まないので、チームで必要になったら自分で加える。

### マシンに追加する

```sh
cclayer layer add acme git@github.com:you/cclayer-acme.git
```

名前はチームの短い名前（小文字、数字、ハイフン）。場所がリポジトリなら、先にデプロイキーとトークンのどちらを使うか尋ねる（第 7 節を参照）。`--method deploy-key` か `--method token` で先に答えてもよい。公開リポジトリや、git がすでにアクセスできるときは `--method none` でこの質問を飛ばす。場所がディレクトリなら、中にすでに `layer.toml` が必要（初期ファイルを書くのは setup だけ）。認証情報の設定、clone、このマシンで使えるかの確認、blocklist の補充を行い、どこかで失敗すればデバイスマニフェストを元に戻す。そのあと `cclayer apply` を実行する。

`cclayer setup` を再実行し、「レイヤー」の「+ オーバーレイを追加」を選んでもよい。

`~/.config/cclayer/device.toml` を直接編集してもよい：

```toml
layers = ["base", "acme"]
roots = ["~/Projects"]

[clone]
base = "~/.local/share/cclayer/base"
acme = "~/.local/share/cclayer/acme"

[repo]
base = "https://github.com/you/cclayer-base.git"
acme = "git@github.com:you/cclayer-acme.git"
```

それから `cclayer init` で足りないレイヤーを clone し、`cclayer apply` で適用する。

### 確認

```sh
cclayer status                   # 各オーバーレイの下に、属するプロジェクトを一覧
cd ~/Projects/acme-api
git config --get user.email      # me@acme.example になるはず
cat .claude/settings.local.json
cat CLAUDE.local.md
cclayer doctor                   # 身元が正しいか、include ブロックがあるか、除外リストが揃っているかを検査
```

どのオーバーレイにも一致しないリポジトリでは、`default_identity` が空だと git はコミットを拒否し、身元を設定するよう求める。これは意図的で、間違った身元で使うのを防ぐ。こうしたリポジトリで既定としてあるレイヤーの身元を使いたければ、setup の「既定の身元」の行でそれを選ぶか、マニフェストに `default_identity = "personal"` と書く。

### 複数のチーム

チームごとにレイヤーを 1 つずつ追加すればよい。`layers` の順序が適用順序で、後のものが優先するが、1 つのプロジェクトが属するオーバーレイは 1 つだけ。2 つのオーバーレイのパターンが同じリポジトリに合うと、apply はそれを競合として一覧し、パターンを直すよう求める。

## 7. 非公開レイヤーリポジトリの認証情報

チームから支給されたマシンには、普通そのチームから渡された git の認証情報しかなく、自分の非公開レイヤーリポジトリには届かない。`keys setup` は 2 つの方法を提供し、レイヤーごとに別々に選べる：

### Deploy key（自動）

```sh
cclayer keys setup acme --method deploy-key
```

cclayer はこのマシンで ed25519 鍵 `~/.ssh/cclayer-acme` を生成し、`~/.ssh/config` の先頭に管理された `Host cclayer-acme` の節を追加し、レイヤーの URL をこのエイリアス経由に書き換える。`gh` がログイン済みで、かつ自分がそのリポジトリの管理者なら、`gh api` で公開鍵を書き込み権限付きでリポジトリに取り付ける。そうでなければ公開鍵と実行すべきコマンドを表示するので、権限のある場所で実行する。最後に `git ls-remote` で接続できることを確認する。

deploy key 1 つが開けるのはリポジトリ 1 つだけで、どのアカウントにも結び付かない。マシンを失くしたらリポジトリの設定でこの鍵を削除すればよい。ネットワークが 22 番ポートを遮断しているなら `--port-443` を付け、`ssh.github.com:443` 経由にする。

### HTTPS token（手動で作る）

```sh
cclayer keys setup acme --method token
```

cclayer は token を作る手順を表示する：GitHub の fine-grained token のページを開き、リソース所有者にはリポジトリが属するアカウントか組織を選び、リポジトリアクセスは「選択したリポジトリのみ」にしてレイヤーリポジトリにチェックを入れ、権限は Contents の読み書き。作ったら cclayer に貼り付ける。cclayer はまずこの token で一度検証し（どの credential helper も通さない）、リポジトリが明確に拒否したら保存しない。通れば git の credential helper に保存し、このリポジトリのパスにだけ有効で、どのファイルにも書かない。

リポジトリの管理者でない場合や、SSH が遮断されている場合はこちらを使う。

### 確認と削除

```sh
cclayer keys list
cclayer keys remove acme     # 鍵と ssh 設定の節を削除するか、token の捨て方を示す。GitHub 側で取り消すべきものを表示
```

setup でリポジトリへのアクセス方法を選ぶと、これと同じことをする。ディレクトリレイヤーには認証情報が要らないので、setup は尋ねない。

## 8. 日常のコマンド詳説

### apply

```sh
cclayer apply [--pull] [--mcp-force] [--replay-plugins]
cclayer apply --hook
```

順に次のことを行う：

1. ロックを取り、2 つの apply が同時に走るのを防ぐ。
2. `--pull` なら、まず各 git レイヤーの clone をリモートに fast-forward する。未コミットの変更がある、上流ブランチがない、fast-forward できない、オフラインのときはスキップして理由を示す。ディレクトリレイヤーは「nothing to pull」と示す。
3. 各レイヤーに `check` を実行し、レイヤーに入れてはいけない内容を拒否する。
4. ベースレイヤーの `claude/` 以下で `paths` に挙げたファイルを `~/.claude` に写す。前回何を書いたかを記憶している：このマシンで変更され、レイヤー側も変わったファイルは競合とし、対話的なら上書きするか尋ね、`--hook` は触らずに名前を挙げる。このマシンで削除されたファイルはこのマシンの変更とみなす：レイヤーが変わっていなければ書き直さず、レイヤーも変わっていれば競合。上書きするファイルは先にバックアップする。
5. `settings.json` をマージする：`settings_keys` に挙げたキーだけを動かし、他のキーはそのまま残す。外観、モデル、`attribution`、`permissions.deny` のような無害とわかっているキーはそのままマージする。制限するだけのキーは、レイヤーが制限を保つか増やすときは確認なしでマージし、消したり緩めたりするときは確認する。レイヤーがキーをオフのまま保つだけ（デバイスで未設定かオフのキーに `false`）なら確認なしでマージする。ただし `false` にすると制限が緩むスイッチは除く。それ以外のキー、特に Claude Code にプログラムを実行させるもの（`hooks`、`statusLine`、`apiKeyHelper` などのヘルパーコマンド、`extraKnownMarketplaces`、`enabledPlugins`）は変更があれば先に表示して確認する。ミラー対象のうち、`hooks/`、`skills/`、`commands/`、`agents/` 以下のファイル、markdown 以外のファイル、実行ビットか shebang を持つファイルに変更があるときも、内容を表示して確認する。
6. `~/.gitconfig.cclayer` と、オーバーレイごとに 1 つの `~/.gitconfig.cclayer.d/<layer>.gitconfig` を生成し、`~/.gitconfig` の末尾にマーカー付きの include ブロックを維持する。他の内容には触れない。ブロックは include するパスで見分けるので、目印のコメントが消えていても置き換え、二重には追加しない。
7. プラグインとマーケットプレイス：ベースレイヤーの一覧に従って実行する `claude plugin` コマンドを列挙し、一度確認してから実行する。マシンごとに一度だけ実行し、`--replay-plugins` で再実行する。マシンに登録済みのマーケットプレイスと user スコープでインストール済みのプラグインはどちらの場合も飛ばす。再登録すると `autoUpdate` のような設定が消えるため。オーバーレイのプラグインは `--scope local` で一致したプロジェクトに入れる。
8. MCP サーバー：ベースレイヤーの `mcp/<name>.json` が 1 ファイル 1 サーバー。`claude mcp get` が知らないものだけ追加し、`--mcp-force` で削除して再追加する。秘密は平文で書かず、`${VAR}` のような環境変数参照として書く。
9. 一致した各プロジェクトに注入ファイルを書き、グローバルな git の除外リストに登録する。オーバーレイがプロジェクトに書くこの種のキーも同様に先に表示して確認する。一致しなくなったプロジェクトからは注入した内容を取り除く。
10. profiles が有効なら、ベースレイヤーを各 profile ディレクトリにコピーする。

`--hook` は SessionStart hook 向けの非対話モード。何も尋ねず、確認が要ることはすべてスキップして一覧する。

### capture

```sh
cclayer capture [--add <~/.claude からの相対パス>]... [--from <プロジェクトディレクトリ>]
```

apply の逆方向。このマシンの `~/.claude` 以下、レイヤーが管理するパスの中で、前回の apply が書いた内容と異なるファイルを、所属するレイヤーに書き戻す。このマシンにしかない新しいファイルは一覧するだけで触らず、`--add` で取り込む。ファイル 1 つでも、ディレクトリ（その下の新しいファイルすべて）でもよい。`settings.json` の `settings_keys` のキーは変更があれば書き戻す。

一致したプロジェクト内の `.claude/settings.local.json` と `CLAUDE.local.md` の変更もオーバーレイに書き戻す。複数のプロジェクトで内容が食い違うときは拒否し、`--from` でどのプロジェクトを採るかを指定する。

### push

```sh
cclayer push [--add <パス>]... [-m <メッセージ>] [--yes]
```

capture を実行し（`--add` は capture と同じ）、続けてすべてのレイヤーに `check` をかける。clone に手で加えた変更があるかもしれないため。各レイヤーのリポジトリの変更を一覧して一度だけ尋ね、指定のメッセージか「cclayer: capture from <ホスト名>」でコミットし、rebase で pull してから push する。コミット済みでまだ push していないものも一緒に送る。リモートと競合したらそのレイヤーの rebase を取り消し、マージすべき clone を示す。ほかのレイヤーはそのまま push する。ディレクトリレイヤーは飛ばす。

各ファイルは先に `check` を通り、拒否されたものは書かずに理由を報告する。書き終えてもレイヤーディレクトリ内のファイルを変えただけで、git レイヤーは自分でコミットして push する。

### check

```sh
cclayer check
```

各レイヤーディレクトリを検査する。すべてのレイヤーで拒否する内容：

- 秘密情報の形をした文字列（各種 token、`KEY=`、JSON 内の秘密の値）
- git フラグメント内の `[user]`、リモート URL
- Claude Code の実行時ファイル、`projects/`

ベースレイヤーではさらに拒否する：

- メールアドレス（`example.com` のような例示ドメインを除く）。ベースの `layer.toml` が `private = true` を宣言していればこの項目は調べない
- デバイスマニフェストの `blocklist` にある語。`blocklist` は `init`/`setup` がオーバーレイから自動で埋める：レイヤー名、コミッターの名前、メールアドレスのローカル部とドメイン、リモートパターン内の組織名。これでチーム名が公開のベースレイヤーに混ざらない。1 文字の語は入れない。ある語がベースレイヤーに入るべきとき（自分の公開プラグインマーケットプレイスを持つ GitHub ユーザー名など）は、デバイスマニフェストの `blocklist_except` に書く。`init` はそれを `blocklist` に加えず、`check` も通す
- `[includeIf]`、ホームディレクトリを指す絶対パス

apply と capture はどちらも自動でこれを実行する。

### status

レイヤーごとに 1 行：ディレクトリ、ローカルディレクトリかどうか、未コミットの変更があるか、リモートより何コミット進んでいるか、遅れているか。次にオーバーレイごとに属するプロジェクトを一覧し、さらに一致しなかったものと競合しているものを一覧する。

### doctor

既知の落とし穴を検査し、直し方を示す：`~/.claude` が git リポジトリになっていないか、シンボリックリンクがないか、`CLAUDE_CONFIG_DIR` が正しく設定されているか、`~/.gitconfig` の include ブロックが末尾にあるか、グローバルな除外リストが揃っているか、一致した各プロジェクトの git の身元が正しいか、`claude` が PATH にあるか。profiles が有効なときは、各 profile ディレクトリにシンボリックリンクがないか、`CLAUDE.md` がないかも検査する（`~/.claude/CLAUDE.md` はもともと読み込まれるので、profile にもう 1 つ置くと 2 回読み込まれる）。

### leave

```sh
cclayer leave acme [--force]
```

あるチームの仕事をしなくなったときに使う。まず片付けるプロジェクトを一覧して確認を求め、それから：各プロジェクトに `claude purge` を実行して Claude Code のプロジェクト状態を消し、profile ディレクトリを削除し、注入したファイルを取り除き、このレイヤーの認証情報を削除し、このレイヤーを除いて git 設定を再生成し、マニフェストを保存し、最後に clone を削除する。clone に未コミットや未 push の変更があるときは拒否し、`--force` のときだけ削除する。レイヤーが自分で指定したディレクトリなら削除せず、その場に残す。終わりに、GitHub で認証情報を取り消すなど、手で行うべきことを一覧する。

### init

自分で書いた `device.toml` を読み、足りないレイヤーを clone し、`blocklist` を埋める。尋ねるのは自動 pull を有効にするかだけ。`device.toml` がなければ、1 行ずつの質問でレイヤーの URL、プロジェクトのディレクトリ、既定の身元を尋ねて書き出す。そのあと `apply` は自分で実行する。

## 9. Profiles：チームごとに独立した Claude Code ディレクトリ

既定ではすべてのプロジェクトが `~/.claude` を共有し、ログイン、セッション記録、プロンプト履歴も 1 組。チームごとに分けたければ、デバイスマニフェストで `profiles = true` にする（setup でも切り替えられる）。以後 apply はベースレイヤーを `~/.claude-profiles/<layer>/` にコピーし、オーバーレイごとに 1 組になる。`CLAUDE.md` は除く。Claude Code は常に `~/.claude` から読むため。

Claude Code を起動するとき、対応するディレクトリを使わせる：

```sh
cd ~/Projects/acme-api
cclayer env                 # export CLAUDE_CONFIG_DIR=... を表示。direnv やシェル用
eval "$(cclayer env)"
cclayer run -- claude       # または変数を付けて直接起動
```

`profile_dir` で既定の場所を変えられる。`leave` のとき対応する profile ディレクトリも一緒に削除される。

## 10. SessionStart hook：セッション開始時に自動 apply

次をベースレイヤーの `claude/settings.json` に入れる。`hooks` が `settings_keys` に入っている必要がある：

```json
{
  "hooks": {
    "SessionStart": [{
      "matcher": "startup",
      "hooks": [{ "type": "command", "command": "command -v cclayer >/dev/null && cclayer apply --hook || true" }]
    }]
  }
}
```

デバイスマニフェストで `auto_pull = true` なら、Claude Code のセッションを開くたびに先に `apply --hook` する。`false` ならこの hook は何もしない。cclayer が入っていないマシンでは hook はそのまま通る。

## 11. ファイルの場所

| パス | 何か |
|---|---|
| `~/.config/cclayer/device.toml` | デバイスマニフェスト。このマシンにだけある |
| `~/.local/share/cclayer/<layer>/` | git レイヤーの clone |
| `~/.local/state/cclayer/` | 前回の apply の記録、ロック |
| `~/.local/state/cclayer/backups/` | 上書きしたファイルのバックアップ。実行ごとに 1 ディレクトリ、直近 5 回と初回を残す |
| `~/.gitconfig.cclayer` | cclayer が生成する git の主設定 |
| `~/.gitconfig.cclayer.d/<layer>.gitconfig` | オーバーレイごとの身元とフラグメント |
| `~/.gitconfig` 末尾のマーカーブロック | cclayer が変更する唯一の場所 |
| `~/.ssh/cclayer-<layer>`、`~/.ssh/config` 先頭のマーカーブロック | deploy key 方式の鍵と ssh エイリアス |
| `~/.claude-profiles/<layer>/` | profiles モードの設定ディレクトリ |

環境変数 `CCLAYER_DEVICE` と `CCLAYER_STATE` でマニフェストと状態ディレクトリの場所を変えられる。`CCLAYER_LANG` は表示言語（`en`、`zh`、`ja`）を選ぶ。設定しなければ、デバイスマニフェストの `lang`、次にシステムの言語で決まる。エラーメッセージは検索しやすいよう常に英語。

## 12. よくある質問

**apply があるファイルを競合だと言う。** このマシンとレイヤーの両方でそれを変えた。対話的に実行するとレイヤー側の版でこのマシンを上書きするか尋ねる。このマシンの版を残したければいいえと答え、`capture` でレイヤーに書き戻す。

**apply が git フラグメントを拒否し、trust_exec と表示する。** フラグメントに `core.hooksPath` のような git にプログラムを実行させるキー、または cclayer が知らないキーがある。このレイヤーが自分の管理下にあることを確認してから、レイヤー名をデバイスマニフェストの `trust_exec` に追加する。

**capture がファイルを拒否した。** 報告されたルールを見る。秘密は `${VAR}` にして環境変数から読む。メールアドレスはオーバーレイの `[identity]` に置く。チーム名はベースレイヤーに現れてはならない。

**どのオーバーレイにも一致しないリポジトリで git がコミットさせない。** `default_identity` が空のときは意図的な動作。このリポジトリ用にオーバーレイを追加して一致パターンを書くか、`default_identity` を設定する。

**status が `local directory` と言う。** このレイヤーはディレクトリレイヤーで、git を使わない。履歴とマージが欲しければ、第 5 節のとおりディレクトリをリポジトリに push し、`cclayer setup` でこのレイヤーの場所をリポジトリの URL に変えて cclayer に clone させる。ディレクトリレイヤーに `.git` があっても cclayer はその中で git を実行しない。その `.git/config` は cclayer が書いたものではなく、git にプログラムを実行させる設定が入っているかもしれないため。

**クラウドドライブで同期するディレクトリを両側で同時に変えた。** クラウドドライブの処理に従い、cclayer はマージしない。マージしたければ git を使う。

**Windows。** Homebrew はないので Releases からダウンロードする。Windows では `cclayer env` は PowerShell の構文で表示する。

**setup で入力を間違えた。** その項目をもう一度選んで入れ直せばよい。保存するまでは何も書き込まれていない。今回誤って追加したオーバーレイは、右の列の「このオーバーレイを削除」で消せる。保存済みのオーバーレイを外すには `cclayer leave <layer>` を使う。

**setup をパイプやスクリプトで実行する。** `--accessible` を付けると、1 行ずつ尋ねるプレーンテキストモードになる。標準入力が端末でないときは自動で切り替わる。
