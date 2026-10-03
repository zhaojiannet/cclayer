# cclayer

在多台机器之间同步 Claude Code 配置：`CLAUDE.md`、规则、skills、hooks、`settings.json`、插件和 MCP server。每个团队专属的部分，包括它的 git 提交身份，只出现在它自己的项目里。

[English](README.md) · [日本語](README.ja.md) · [详细教程](docs/tutorial.zh.md)

你在不止一台机器上用 Claude Code，手头的项目也来自不止一个团队。全局规则、技能、插件、权限这些配置，你希望改一次处处生效；而每个团队的 git 提交身份、专用规则和 hook，各自只在这个团队的项目和机器上生效，不会混进别的项目。

cclayer 把配置拆成两种 git 仓库：一个**公开的基础层**放各处相同的部分，每个团队一个**私有的覆盖层**放只属于它的部分。每台机器只拉基础层加它被允许看到的覆盖层，一条命令铺好，一条命令把本机的改动写回去。

## 只同步自己的配置

没有要分开的团队？一个基础层就够了：

```sh
cclayer setup     # 地址填一个新目录（网盘同步文件夹也行）或一个私有 git 仓库
cclayer capture --add CLAUDE.md --add rules/ --add skills/   # 把这台机器现有的配置收进去
```

其他机器上 `cclayer setup` 填同一个地址，`cclayer apply` 就把配置铺过来。只有你自己用、放的地方也私有时，在基础层 `layer.toml` 的 `[layer]` 下加 `private = true`，`check` 就不再拦邮箱。[详细教程](docs/tutorial.zh.md#4-最简单的用法一个人几台机器用目录同步)一步步讲了这个用法。

## 原理

**层**就是一个根目录带 `layer.toml` 的目录。通常是一个 git 仓库，由 cclayer clone 到本机；也可以直接是本机的一个目录，比如放在网盘同步文件夹里，这时不需要 git。

- **基础层**放每台设备都要的东西：`CLAUDE.md`、`rules/`、`skills/`、`output-styles/`、`agents/`、hook 脚本、`settings.json` 的共享键、插件和 marketplace 清单、MCP 定义。里面没有任何身份信息，所以可以公开。
- **覆盖层**放一家组织的 git 身份、hook、它的仓库远程地址模式，以及要写进这些仓库工作区的文件：`.claude/settings.local.json` 的键和 `CLAUDE.local.md` 里的受管区块。覆盖层从不往 `~/.claude/` 写东西。

设备清单 `~/.config/cclayer/device.toml` 记录这台机器启用了哪些层、clone 在哪、项目在哪找，它不进任何仓库。运行时状态（`~/.claude.json`、登录、`history.jsonl`、`projects/`）不会被读进层里；`permissions.allow`、`permissions.ask`、`permissions.defaultMode`、`env` 留在设备上。

## 安装

```sh
brew install --cask zhaojiannet/tap/cclayer      # macOS
brew upgrade --cask cclayer                      # 以后升级到新版本
```

Linux 和 Windows 的二进制在 [Releases 页面](https://github.com/zhaojiannet/cclayer/releases)。需要 git；插件、MCP 和 purge 这几步还需要 Claude Code 2.1.288 以上。

## 用法

```sh
cclayer setup            # 首次配置：各层、项目目录、凭证、apply、doctor
cclayer init             # 按你自己写好的清单 clone 各层
cclayer apply            # 把各层铺到这台设备
cclayer capture          # 把本机改动写回层的 clone
cclayer check            # 拒绝不该进层的内容
cclayer status           # 各层的 git 状态和匹配到的项目
cclayer keys setup <层>  # 给这台设备配一个层仓库的凭证
cclayer layer add <名字> <地址>  # 给这台设备加一个覆盖层
cclayer leave <层>       # 清掉一个层的项目状态并从本机移除它
cclayer doctor           # Claude Code 和 git 的已知坑
cclayer env | run        # 按团队选 CLAUDE_CONFIG_DIR（profiles 模式）
cclayer version
```

`setup` 是一个全屏界面。左栏列出这台设备的各层（git 地址或本机目录，填一个还不存在的目录会生成起步的 `layer.toml`）和只属于本机的设置：项目目录、没匹配到的仓库用什么身份（有覆盖层时才有这一项）、自动拉取、profiles、信任哪些层、界面语言。右栏显示选中项的说明和字段（覆盖层的连接方式，或从它的 `layer.toml` 读出的身份和匹配规则），并说明保存时会做什么。底部一行写着还差什么，旁边是「保存并应用」「只保存」「退出」三个按钮，保存之前什么都不写。保存后写设备清单、配凭证、clone 各层、apply、跑 `doctor`。再跑一次 `setup`，打开时显示已保存的值。`layer add <名字> <地址或目录>` 不进这一页，直接加一个覆盖层：配凭证、clone、检查、补 blocklist，哪一步失败都把设备清单恢复原样。地址是仓库时会问怎么配凭证，公开仓库或 git 已经能访问时加 `--method none`；地址是目录时，里面必须已经有 `layer.toml`，起步文件只有 setup 会写。`init` 读你自己写好的清单，clone 其中的层并填好 blocklist，只问一句要不要开自动拉取；清单不存在时，它用逐行提问问出各层地址、项目目录和默认身份，再写出清单。之后由你自己跑 `apply`。加 `--accessible` 用纯文本逐行提问代替全屏界面；标准输入不是终端时自动切换。

界面文字有英文、简体中文、日文三种。先看环境变量 `CCLAYER_LANG`，再看设备清单里的 `lang = "zh"`（或 `en`、`ja`），最后看系统语言：`LC_ALL`、`LC_MESSAGES`、`LANG` 里第一个有值的那个，cclayer 没有的语言用英文。报错信息始终是英文，方便搜索。

`apply` 先跑 `check`，然后把基础层镜像进 `~/.claude`，合并 `settings.json` 里归属的键，重新生成 `~/.gitconfig.cclayer` 并在 `~/.gitconfig` 末尾维护一个 include 块，确认一次后重放插件和 MCP server，再把覆盖层的文件写进每个匹配的项目。它记得自己写过什么：设备上改过、层里也变了的文件算冲突，交互式 `apply` 会问，`apply --hook` 不碰。被覆盖的文件备份在 `~/.local/state/cclayer/backups/`。`apply --pull` 先把每个 git 层的 clone 快进到远程：有未提交改动、没有上游分支、没网、不是快进的，报出来并跳过；目录层没有可拉取的内容。

`capture` 是反方向：设备上的改动和上次 `apply` 写的不一样时写回所属的层，每个文件先过 `check`。只在设备上有的文件会列出来，用 `--add <文件或目录>` 收进去。匹配项目里改过的注入文件也写回所属覆盖层；几个项目改得不一致时，用 `--from <项目>` 指定以哪个为准。`capture` 不提交。

`leave` 对该层每个项目跑 `claude purge`，删掉注入的文件和这一层的凭证，去掉这一层重新生成 git 配置，保存清单，最后删 clone。clone 里有未提交或未推送的改动时拒绝，加 `--force` 才删。层是你自己指定的目录时不删，原地保留。

插件和 marketplace 的命令每台设备只跑一次，`apply --replay-plugins` 重跑。基础层的市场和 user 范围的插件，本机已有的无论如何都跳过，因为重新登记市场会冲掉 `autoUpdate` 这类设置。MCP server 只在 `claude mcp get` 不认识时才添加，`--mcp-force` 强制替换。

### 层仓库的凭证

一台设备只需要够到它的层仓库。`keys setup <层>` 给两条路，同一台设备可以混用：

- **Deploy key**（`--method deploy-key`）：本机生成 ed25519 钥匙到 `~/.ssh/cclayer-<层>`，往 `~/.ssh/config` 加一段 `Host cclayer-<层>`（`--port-443` 走 `ssh.github.com:443`），把层地址改成走别名，然后在 `gh` 已登录且是该仓库管理员时用 `gh api` 把公钥挂到仓库并给写权限；否则把公钥和那条命令打印出来让你在别处跑。一把 deploy key 只能打开一个仓库，不绑任何账号。
- **HTTPS token**（`--method token`）：你在 GitHub 上创建 fine-grained token（资源所有者、只勾选的仓库、Contents 读写），cclayer 把它存进 git 的凭证助手，只对那个仓库路径生效，不写进任何文件。你不是仓库管理员、或者 SSH 被封时用这条。

deploy key 这条路最后用 `git ls-remote` 验证能不能连上。token 这条路先验证再存：验证经 `GIT_ASKPASS` 进行、不经过任何凭证助手，仓库明确拒绝时不存；只是网络不通则照存并说明。`keys list` 看每层用的方式；`keys remove <层>` 删钥匙文件和 ssh 配置段，或告诉你怎么丢掉 token，并打印要在 GitHub 上吊销什么。你所服务的团队的仓库用他们发给你的凭证，cclayer 只碰自己的层仓库。

### Profiles

设备清单里 `profiles = true` 后，每个覆盖层有自己的 Claude Code 配置目录 `~/.claude-profiles/<层>`（登录、会话、提示词历史按团队分开；清单里的 `profile_dir` 可以换上级目录）。`apply` 把基础层的镜像目录（`CLAUDE.md` 除外，Claude Code 总会从 `~/.claude` 读它）和归属的 settings 键复制进每个 profile，复制而不是链接。`cclayer env [目录]` 打印匹配该项目的覆盖层对应的 `export CLAUDE_CONFIG_DIR=...`（给 direnv 或你的 shell 用），`cclayer run [目录] -- claude` 带着它启动命令。

### 设备清单

```toml
layers = ["base", "personal", "acme"]    # base 在前，后面的层优先
default_identity = ""                    # 留空：没有覆盖层匹配的仓库 git 拒绝提交
roots = ["~/Projects"]                   # 在哪里找项目
auto_pull = false                        # 为 false 时 SessionStart hook 什么都不做
profiles = false                         # 每个覆盖层一个 Claude Code 配置目录，见 Profiles
profile_dir = "~/.claude-profiles"       # 这些目录放在哪
blocklist = ["acme", "acme-inc"]         # init 从覆盖层自动填
blocklist_except = []                    # 基础层可以出现的词，init 不会把它们加进 blocklist
trust_exec = []                          # 允许在 git 片段里设能让 git 执行程序的键的层

[clone]
base = "~/.local/share/cclayer/base"
acme = "~/.local/share/cclayer/acme"

[repo]
base = "https://github.com/you/cclayer-base.git"
acme = "git@github.com:you/cclayer-acme.git"
```

`[repo]` 里没有条目的层就是一个本机目录：`[clone]` 指到哪，cclayer 就直接读写哪，不 clone、不拉取，`status` 标为 `local directory`，`leave` 不删它。即使里面有 `.git`，cclayer 也不在其中运行 git，因为那份配置不是 cclayer 写的。把这个目录放进网盘的同步文件夹，几台机器各自 `setup` 时填同一个路径，就能不经 git 同步。路径还不存在时 `setup` 会写一份起步的 `layer.toml`。环境变量 `CCLAYER_DEVICE` 改清单位置，`CCLAYER_STATE` 改状态目录（默认 `~/.local/state/cclayer`），`CCLAYER_LANG` 改界面语言。

### 基础层

```
layer.toml
claude/                 镜像进 ~/.claude：CLAUDE.md、rules/、skills/、hooks/……
claude/settings.json    settings_keys 里列出的键
mcp/<name>.json         一个文件一个 MCP server，密钥用 ${VAR}
git/fragment.gitconfig  可选的原始 git 配置；不能有 [user] 和远程地址
```

```toml
[layer]
name = "base"
kind = "base"
private = false          # true：只有自己用、放在私有的地方，check 不拦邮箱

[claude]
paths = ["CLAUDE.md", "rules/", "skills/", "output-styles/", "hooks/", "statusline.sh"]
settings_keys = ["attribution", "permissions.deny", "hooks", "statusLine",
  "enabledPlugins", "extraKnownMarketplaces", "outputStyle", "effortLevel", "theme"]
ignore = ["skills/.trash/", "skills/synced/"]

[git]
fragment = "git/fragment.gitconfig"
```

### 覆盖层

```
layer.toml
project/settings.local.json   写进匹配项目的键
project/CLAUDE.local.md       受管区块
git/fragment.gitconfig        可选的 hook 和 alias，只在匹配的仓库里生效
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

匹配模式必须写明主机和组织，比如 `github.com/acme-inc/*`，不能用通配符代替这两段，免得一个覆盖层认领别的团队的仓库；两个覆盖层也不能写同一个主机加组织。项目的任一 git 远程地址符合模式就算匹配，不分大小写，和代码托管平台对组织名、仓库名的处理一致；同时符合两个覆盖层的项目会被报出并跳过。覆盖层的插件用 `claude plugin install --scope local` 装在匹配的项目里。

git 片段里可以放逐个列出的常见工作流设置（`pull.rebase`、`push.default`、`merge.conflictstyle`、`diff.algorithm`、`color.ui` 等）；其余的键，包括 alias，一律拒绝，除非这一层列在设备清单的 `trust_exec` 里。alias 能执行任意程序，`core.hooksPath`、`credential.helper`、`core.sshCommand`、`url.*.insteadOf` 等也一样。写进生成的 git 配置的，是 git 解析片段后重新写出的内容。设置键同理：外观、模型、上下文和工作流偏好、`attribution`、`permissions.deny` 以及只会限制 Claude Code 的键直接合并；其他键，特别是会执行程序的（`hooks`、`statusLine`、`apiKeyHelper` 等助手命令键）、会自动注册市场的 `extraKnownMarketplaces` 和会启用插件的 `enabledPlugins`，不论基础层写进 `~/.claude/settings.json` 还是覆盖层写进项目的 `settings.local.json`，有变化时交互式 `apply` 先展示再确认，`apply --hook` 跳过。只做限制的键（`permissions.deny`、`disableAllHooks` 等）只有在层保持或增加本机已有的限制时才直接合并，删除或放宽限制也要确认。层只是让某个键保持关闭（本机没设或本来就关着，层也设成 `false`）时，不会打开任何东西，直接合并；`disable*`、`permissions` 和 `sandbox` 下的键以及 `respectGitignore`、`useAutoModeDuringPlan` 除外，这些设成 `false` 反而是放宽限制。会被 Claude Code 执行的镜像文件同样处理：`hooks/`、`skills/`、`commands/`、`agents/` 下的所有文件，所有非 markdown 文件，以及带执行位或 shebang 的文件，写入前先列出内容再确认，层的版本要覆盖本机改动时也一样。同一路径、同一内容在本机确认过一次后不再重复问。写文件从不跟随 `~/.claude`、层 clone 和项目工作区里的符号链接；层里只能放普通文件，层目录以下任何位置出现符号链接，`apply` 和 `check` 都会停下，`capture` 也绝不会经由链接往外写。

### git 身份

cclayer 拥有 `~/.gitconfig.cclayer` 和 `~/.gitconfig.cclayer.d/` 下每个覆盖层一个文件。`~/.gitconfig` 只在末尾多一个 include 块，其余内容包括 credential helper 一律不动。`apply` 按 include 的路径认出这个块，标记注释丢了也能认出来替换掉，不会再加一份。每个覆盖层的身份、hook 和 alias 靠 `includeIf "hasconfig:remote.*.url:..."` 只在远程地址匹配的仓库里生效。`default_identity` 留空时，`user.useConfigOnly` 让 git 在没有覆盖层匹配的仓库里拒绝提交。

### SessionStart hook

放进基础层的 `claude/settings.json`，`auto_pull = true` 之后每个会话开始时拉取并应用：

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

### 凭证

cclayer 只管它自己的层仓库的凭证，也就是上面 `keys setup` 那一节；本机目录的层不需要凭证。Claude Code 的登录和你所服务团队发给你的仓库凭证，它不读、不复制、不写。

## 开发

```sh
cp .env.example .env
docker compose up -d
docker compose exec app go test ./...
```

MIT 协议。
