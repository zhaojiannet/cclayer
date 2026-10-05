<p align="center"><img src="assets/logo.png" alt="cclayer のロゴ：ベースレイヤーとチームのオーバーレイを同期" width="160"></p>

# cclayer

Claude Code の設定を複数のマシンで同期する：`CLAUDE.md`、ルール、skills、hooks、`settings.json`、プラグイン、MCP サーバー。あるチーム固有の部分は、git の身元も含めて、そのチームのプロジェクトにだけ現れる。

[English](README.md) · [中文](README.zh.md) · [チュートリアル](docs/tutorial.ja.md)

Claude Code を複数のマシンで使い、プロジェクトは複数のチームから来る。グローバルなルール、スキル、プラグイン、権限は一度変えればどこでも反映されてほしい。一方、各チームの git の身元、ルール、hook は、そのチームのプロジェクトとマシンでだけ有効になり、別のチームのものと混ざらない。

cclayer は設定を 2 種類の git リポジトリに分ける。どこでも同じ部分を入れる**公開のベースレイヤー**が 1 つ、そのチームだけのものを入れる**非公開のオーバーレイ**がチームごとに 1 つ。各マシンはベースと、見てよいオーバーレイだけを pull する。1 コマンドで適用し、1 コマンドでローカルの変更を書き戻す。

<p align="center"><img src="assets/setup.png" alt="cclayer setup の画面：左にレイヤーとマシンの設定、右に選んだ項目の詳細、下に保存ボタン" width="800"></p>

## 自分の設定だけを同期する

分けるべきチームがないなら、ベースレイヤー 1 つで足りる：

```sh
cclayer setup     # 場所には新しいディレクトリ（クラウド同期フォルダでもよい）か非公開の git リポジトリを入れる
cclayer capture --add CLAUDE.md --add rules/ --add skills/   # このマシンにある設定を取り込む
```

ほかのマシンで同じ場所を指定して `cclayer setup` を実行すれば、`cclayer apply` で設定が届く。自分だけが使い、置き場所も非公開なら、ベースの `layer.toml` の `[layer]` に `private = true` を書く。`check` はメールアドレスを通すようになる。[チュートリアル](docs/tutorial.ja.md#4-最も簡単な使い方1-人数台のマシンディレクトリ同期)で手順を説明している。

## 仕組み

**レイヤー**はルートに `layer.toml` を持つディレクトリ。通常は cclayer が clone する git リポジトリだが、このマシン上の普通のディレクトリでもよい。たとえばクラウドドライブが同期するフォルダの中に置けば、git なしで使える。

- **ベース**レイヤーはすべてのデバイスに入るもの：`CLAUDE.md`、`rules/`、`skills/`、`output-styles/`、`agents/`、hook スクリプト、`settings.json` の共有キー、プラグインとマーケットプレイスの一覧、MCP 定義。身元情報を含まないので公開できる。
- **オーバーレイ**は 1 つの組織の git の身元、hook、その組織のリポジトリのリモート URL パターン、そしてそれらのリポジトリの作業ツリーに書くファイル：`.claude/settings.local.json` のキーと `CLAUDE.local.md` の管理ブロック。オーバーレイは `~/.claude/` に何も書かない。

デバイスマニフェスト `~/.config/cclayer/device.toml` は、このマシンで有効なレイヤー、clone の場所、プロジェクトを探す場所を記録する。どこにもコミットしない。実行時状態（`~/.claude.json`、ログイン、`history.jsonl`、`projects/`）はレイヤーに読み込まれず、`permissions.allow`、`permissions.ask`、`permissions.defaultMode`、`env` はデバイスに残る。

## インストール

```sh
brew install --cask zhaojiannet/tap/cclayer      # macOS
brew upgrade --cask cclayer                      # 新しいバージョンへの更新
```

Linux と Windows のバイナリは [Releases ページ](https://github.com/zhaojiannet/cclayer/releases) にある。git が必要。プラグイン、MCP、purge の手順には Claude Code 2.1.288 以降も必要。

## 使い方

```sh
cclayer setup            # 初期設定：レイヤー、ルート、認証情報、apply、doctor
cclayer init             # 自分で書いたマニフェストのレイヤーを clone する
cclayer apply            # レイヤーをこのデバイスに適用
cclayer capture          # ローカルの編集をレイヤーの clone に書き戻す
cclayer push             # capture してから、各レイヤーのリポジトリをコミットして push
cclayer check            # レイヤーに入れてはいけない内容を拒否
cclayer status           # レイヤーごとの git 状態と一致したプロジェクト
cclayer keys setup <layer>   # このデバイスに 1 つのレイヤーリポジトリの認証情報を与える
cclayer layer add <name> <url>  # このデバイスにオーバーレイを 1 つ追加する
cclayer leave <layer>    # レイヤーのプロジェクト状態を消し、このデバイスから外す
cclayer doctor           # Claude Code と git の既知の落とし穴
cclayer env | run        # チームごとの CLAUDE_CONFIG_DIR を選ぶ（profiles モード）
cclayer version
```

`setup` は全画面の編集画面。左の列にはこのデバイスのレイヤー（git の URL かローカルディレクトリ。まだないパスなら初期の `layer.toml` を書く）と、マシン自体の設定（プロジェクトのルート、一致しないリポジトリの身元（オーバーレイがあるときだけ）、自動 pull、profiles、信頼するレイヤー、表示言語）が並ぶ。右の列は選んだ項目の説明と欄（オーバーレイのアクセス方法、または `layer.toml` から読んだ身元と一致ルール）を表示し、保存すると何が起きるかも示す。最下行には足りないものが表示され、その横に「保存して適用」「保存のみ」「終了」のボタンがある。保存するまでは何も書き込まない。保存したらデバイスマニフェストを書き、認証情報を設定し、レイヤーを clone し、apply と `doctor` を実行する。`setup` をもう一度実行すると、保存済みの値で開く。`layer add <name> <url|directory>` はこのページを使わずにオーバーレイを 1 つ追加する。認証情報、clone、チェック、blocklist の補充を行い、どこかで失敗すればマニフェストを元に戻す。場所がリポジトリなら認証情報の設定方法を尋ねる。公開リポジトリや、git がすでにアクセスできる場合は `--method none` を付ける。場所がディレクトリなら、中にすでに `layer.toml` が必要（初期ファイルを書くのは setup だけ）。`init` は自分で書いたマニフェストを読み、載っているレイヤーを clone して blocklist を埋める。尋ねるのは自動 pull を有効にするかだけ。マニフェストがなければ、1 行ずつの質問でレイヤーの URL、プロジェクトのルート、既定の身元を尋ねてマニフェストを書く。そのあと `apply` は自分で実行する。`--accessible` を付けると全画面の代わりに 1 行ずつのプレーンな質問になる。標準入力が端末でないときは自動で切り替わる。

表示は英語、簡体字中国語、日本語の 3 種類。まず環境変数 `CCLAYER_LANG`、次にデバイスマニフェストの `lang = "ja"`（または `en`、`zh`）、最後にロケールで決まる。ロケールは `LC_ALL`、`LC_MESSAGES`、`LANG` のうち最初に値があるもので、cclayer にない言語なら英語になる。エラーメッセージは検索しやすいよう常に英語。

`apply` はまず `check` を実行し、ベースを `~/.claude` に写し、`settings.json` の所有キーをマージし、`~/.gitconfig.cclayer` と `~/.gitconfig` 末尾の include ブロックを再生成し、一度の確認のあとプラグインと MCP サーバーを再適用し、一致した各プロジェクトにオーバーレイのファイルを書く。前回書いた内容を記憶していて、デバイス側で編集され、かつレイヤー側も変わったファイルは競合として扱う。対話的な `apply` は確認し、`apply --hook` は触らない。上書きしたファイルは `~/.local/state/cclayer/backups/` にバックアップされる。`apply --pull` は先に各 git レイヤーの clone を fast-forward する。未コミットの変更がある、上流ブランチがない、ネットワークがない、fast-forward できない clone は報告してスキップし、ローカルディレクトリには pull するものがない。

`capture` はその逆。前回の `apply` が書いた内容と異なるデバイス側の変更を、所有するレイヤーに書き戻す。各ファイルは先に `check` を通る。デバイスにしかないファイルは一覧され、`--add <ファイルかディレクトリ>` で受け入れる。一致したプロジェクト内で編集された注入ファイルも所属するオーバーレイに書き戻す。複数のプロジェクトで内容が食い違うときは `--from <project>` でどれを採るか指定する。`capture` はコミットしない。

`push` は同期のアップロード側で、`apply --pull` がダウンロード側にあたる。`capture` と `check` を実行し、各レイヤーのリポジトリでコミットする内容を一覧して一度だけ尋ね（`--yes` で省略）、コミットする（メッセージは `-m <message>`、なければ「cclayer: capture from <ホスト名>」）。その間にほかのマシンが push したコミットの上に載せ直してから push する。それらのコミットと競合したら clone を元のままにして止まり、手作業でのマージに任せる。ディレクトリレイヤーは対象外で、そのフォルダを同期する仕組みに任せる。

`leave` はそのレイヤーの各プロジェクトに `claude purge` を実行し、注入したファイルとそのレイヤーの認証情報を削除し、レイヤーを除いた git 設定を再生成し、マニフェストを保存し、最後に clone を削除する。clone に未コミットや未 push の変更があるときは `--force` なしでは拒否する。自分で指定したディレクトリのレイヤーは削除せずそのまま残す。

プラグインとマーケットプレイスのコマンドはデバイスごとに一度だけ実行され、`apply --replay-plugins` で再実行できる。ベースレイヤーのマーケットプレイスと user スコープのプラグインは、デバイスに既にあればどちらの場合も飛ばす。マーケットプレイスを再登録すると `autoUpdate` のような設定が消えるため。MCP サーバーは `claude mcp get` が知らないときだけ追加され、`--mcp-force` で置き換える。

### レイヤーリポジトリの認証情報

デバイスに必要なのは自分のレイヤーリポジトリに届くことだけ。`keys setup <layer>` は 2 つの方法を提供し、1 台のデバイスで混在できる。

- **Deploy key**（`--method deploy-key`）：ed25519 鍵を `~/.ssh/cclayer-<layer>` に生成し、`~/.ssh/config` に `Host cclayer-<layer>` の節を追加し（`--port-443` で `ssh.github.com:443` 経由）、レイヤーの URL をそのエイリアス経由に書き換え、`gh` がそのリポジトリの管理者としてログインしていれば `gh api` で公開鍵を書き込み権限付きで取り付ける。そうでなければ公開鍵と実行すべきコマンドを表示する。deploy key は 1 つのリポジトリだけを開き、どのアカウントにも結び付かない。
- **HTTPS token**（`--method token`）：GitHub で fine-grained token を作り（リソース所有者、選択したリポジトリのみ、Contents の読み書き）、cclayer がそれを git の credential helper にそのリポジトリパス向けに保存する。ファイルには書かない。リポジトリの管理者でない場合や SSH が遮断されている場合に使う。

deploy key の方は最後に `git ls-remote` で到達を確認する。token の方は先に検証してから保存する。検証は `GIT_ASKPASS` 経由で credential helper を通さずに行い、リポジトリが明確に拒否したときは保存しない。ネットワークが届かないだけなら保存してその旨を表示する。`keys list` はレイヤーごとの方法を表示し、`keys remove <layer>` は鍵ファイルと ssh の節を削除するか token の捨て方を示し、GitHub 側で取り消すべきものを表示する。所属チームのリポジトリはチームから渡された認証情報のまま。cclayer が触るのは自分のレイヤーリポジトリだけ。

### Profiles

デバイスマニフェストで `profiles = true` にすると、各オーバーレイが `~/.claude-profiles/<layer>` に自分の Claude Code 設定ディレクトリを持つ（ログイン、セッション、プロンプト履歴がチームごとに分かれる。マニフェストの `profile_dir` で親ディレクトリを変えられる）。`apply` はベースレイヤーの写すパス（`CLAUDE.md` は除く。Claude Code は常に `~/.claude` から読む）と所有する settings キーを各 profile にコピーする（シンボリックリンクではなくコピー）。`cclayer env [dir]` はプロジェクトに一致するオーバーレイの `export CLAUDE_CONFIG_DIR=...` 行を表示し（direnv やシェル用）、`cclayer run [dir] -- claude` はそれを設定してコマンドを起動する。

### デバイスマニフェスト

```toml
layers = ["base", "personal", "acme"]    # base が先頭。後のレイヤーが優先
default_identity = ""                    # 空：どのオーバーレイにも一致しないリポジトリでは git がコミットを拒否
roots = ["~/Projects"]                   # プロジェクトを探す場所
auto_pull = false                        # false の間は SessionStart hook は何もしない
profiles = false                         # オーバーレイごとに Claude Code の設定ディレクトリを持つ。Profiles を参照
profile_dir = "~/.claude-profiles"       # それらのディレクトリの置き場所
blocklist = ["acme", "acme-inc"]         # init がオーバーレイから埋める
blocklist_except = []                    # ベースに入ってよい語。init は blocklist に加えない
trust_exec = []                          # git フラグメントでプログラムを実行させるキーを許すレイヤー
clone_dir = "~/.local/share/cclayer"     # リポジトリのレイヤーの clone 先（既定値）

[clone]
base = "~/.local/share/cclayer/base"
acme = "~/.local/share/cclayer/acme"

[repo]
base = "https://github.com/you/cclayer-base.git"
acme = "git@github.com:you/cclayer-acme.git"
```

`[repo]` に項目がないレイヤーはローカルディレクトリ。cclayer は `[clone]` のパスをそのまま読み書きし、clone も pull もしない。`status` は `local directory` と表示し、`leave` は削除しない。中に `.git` があっても cclayer はそこで git を実行しない。その設定は cclayer が書いたものではないため。そのディレクトリをクラウドドライブの同期フォルダに置き、各マシンの `setup` で同じパスを入れれば、git なしで同期できる。まだ存在しないパスを入れると `setup` が初期の `layer.toml` を書く。リポジトリのレイヤーは `~/.local/share/cclayer/<layer>` に clone される。デバイスマニフェストの `clone_dir` か、setup の「ローカルの clone 先」の行で別のフォルダを選べ、保存時に setup が cclayer の clone をそこへ移す。GitHub 上のリポジトリは動かない。環境変数 `CCLAYER_DEVICE` はマニフェストの場所を、`CCLAYER_STATE` は状態ディレクトリ（既定は `~/.local/state/cclayer`）を変える。`CCLAYER_LANG` は表示言語を変える。

### ベースレイヤー

```
layer.toml
claude/                 ~/.claude に写す：CLAUDE.md、rules/、skills/、hooks/ など
claude/settings.json    settings_keys に挙げたキー
mcp/<name>.json         1 ファイル 1 MCP サーバー、秘密は ${VAR}
git/fragment.gitconfig  任意の生の git 設定。[user] とリモート URL は不可
```

```toml
[layer]
name = "base"
kind = "base"
private = false          # true：自分だけが使い置き場所も非公開。check はメールを通す

[claude]
paths = ["CLAUDE.md", "rules/", "skills/", "output-styles/", "hooks/", "statusline.sh"]
settings_keys = ["attribution", "permissions.deny", "hooks", "statusLine",
  "enabledPlugins", "extraKnownMarketplaces", "outputStyle", "effortLevel", "theme"]
ignore = ["skills/.trash/", "skills/synced/"]

[git]
fragment = "git/fragment.gitconfig"
```

### オーバーレイ

```
layer.toml
project/settings.local.json   一致したプロジェクトに書くキー
project/CLAUDE.local.md       管理ブロック
git/fragment.gitconfig        任意の hook と alias。一致したリポジトリでのみ有効
```

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

パターンはホストと組織を文字どおりに書く必要がある（例 `github.com/acme-inc/*`）。この 2 つをワイルドカードにすると、1 つのオーバーレイが他のチームのリポジトリまで取り込めてしまうため。2 つのオーバーレイが同じホストと組織を書くこともできない。プロジェクトは git リモートのどれかがパターンに合えば一致する。ホスティングサービスが組織名とリポジトリ名を大文字小文字で区別しないのに合わせ、比較は大文字小文字を区別しない。2 つのオーバーレイに合うプロジェクトは報告されてスキップされる。オーバーレイのプラグインは一致したプロジェクト内で `claude plugin install --scope local` によりインストールされる。

git フラグメントに置けるのは、キーごとに列挙したよくあるワークフロー設定（`pull.rebase`、`push.default`、`merge.conflictstyle`、`diff.algorithm`、`color.ui` など）。alias を含むそれ以外のキーは、そのレイヤーがデバイスマニフェストの `trust_exec` に載っていなければ拒否される。alias は任意のプログラムを実行でき、`core.hooksPath`、`credential.helper`、`core.sshCommand`、`url.*.insteadOf` なども同様。生成される git 設定に入るのは、git がフラグメントを解析した結果を書き直したもの。設定キーも同じ考え方で、外観、モデル、コンテキストとワークフローの好み、`attribution`、`permissions.deny`、Claude Code を制限するだけのキーはそのままマージする。それ以外のキー、特にプログラムを実行するもの（`hooks`、`statusLine`、`apiKeyHelper` などのヘルパーコマンド）、マーケットプレイスを自動登録する `extraKnownMarketplaces`、プラグインを有効にする `enabledPlugins` は、ベースが `~/.claude/settings.json` に書くものも、オーバーレイがプロジェクトの `settings.local.json` に書くものも、変更があれば対話的な `apply` が表示して確認し、`apply --hook` は飛ばす。制限するだけのキー（`permissions.deny`、`disableAllHooks` など）は、レイヤーがデバイスの制限を保つか増やすときだけ確認なしでマージする。制限を消したり緩めたりする変更は確認する。レイヤーがキーをオフのまま保つだけ（デバイスで未設定かオフのキーに `false` を設定する）なら、何もオンにしないので確認なしでマージする。ただし `disable*`、`permissions` と `sandbox` 以下のキー、`respectGitignore`、`useAutoModeDuringPlan` は除く。これらは `false` にすると制限が緩むため。Claude Code が実行しうるミラー対象ファイルも同じ扱い。`hooks/`、`skills/`、`commands/`、`agents/` 以下のすべてのファイル、markdown 以外のすべてのファイル、実行ビットか shebang を持つファイルは、書き込む前に内容を表示して確認する。レイヤーの版でローカルの変更を上書きするときも同様。このデバイスで一度確認したファイルは、パスと内容が同じあいだは二度尋ねない。書き込みは `~/.claude`、レイヤーの clone、プロジェクトの作業ツリー内のシンボリックリンクを決して辿らない。レイヤーに置けるのは通常のファイルだけで、レイヤーディレクトリ以下のどこかにシンボリックリンクがあれば `apply` も `check` も止まり、`capture` もリンクを経由して外へ書くことはない。

### git の身元

cclayer が所有するのは `~/.gitconfig.cclayer` と `~/.gitconfig.cclayer.d/` 配下のオーバーレイごとのファイル。`~/.gitconfig` には末尾に include ブロックが 1 つ入るだけで、credential helper を含む他の内容には触れない。`apply` は include するパスでこのブロックを見分けるので、目印のコメントが消えていても置き換え、二重には追加しない。各オーバーレイの身元、hook、alias は `includeIf "hasconfig:remote.*.url:..."` を通してリモートが一致するリポジトリでだけ有効になる。`default_identity` が空なら `user.useConfigOnly` により、どのオーバーレイにも一致しないリポジトリで git はコミットを拒否する。

### SessionStart hook

ベースレイヤーの `claude/settings.json` に入れておくと、`auto_pull = true` のあと毎セッション開始時に pull と apply を行う：

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

### 認証情報

cclayer が管理する認証情報は自分のレイヤーリポジトリのものだけで、上の `keys setup` の節がそれにあたる。ローカルディレクトリのレイヤーには認証情報は要らない。Claude Code のログインや、所属チームから渡されたリポジトリの認証情報は、読まず、コピーせず、書かない。

## 開発

```sh
cp .env.example .env
docker compose up -d
docker compose exec app go test ./...
```

MIT ライセンス。
