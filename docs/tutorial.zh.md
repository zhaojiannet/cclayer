# cclayer 使用教程

[English](tutorial.en.md) · [日本語](tutorial.ja.md)

这份教程从零开始讲清楚 cclayer 是什么、怎么装、怎么用。读完 README 还不清楚的，看这里。

## 1. 它解决什么问题

Claude Code 的配置都放在 `~/.claude/` 下：全局规则 `CLAUDE.md`、`rules/`、`skills/`、输出风格、hook 脚本、`settings.json`、插件、MCP server。这些东西你改好一次，就希望每台机器上都一样。

同时你手头的项目可能来自不止一个团队。每个团队有自己的 git 提交身份、自己的专用规则和 hook，这些只该在那个团队的项目里生效，不该混到别的项目里，也不该出现在别的团队的机器上。

cclayer 把配置分成两种「层」：

| 层 | 放什么 | 有几个 | 能不能公开 |
|---|---|---|---|
| 基础层 `base` | 所有机器都一样的部分：`CLAUDE.md`、`rules/`、`skills/`、输出风格、hook 脚本、`settings.json` 里的共享键、插件和 marketplace 清单、MCP 定义 | 1 个 | 可以。里面没有任何身份信息 |
| 覆盖层 | 一个团队专属的部分：git 身份、hook、它的仓库远程地址模式、要写进它项目里的 `.claude/settings.local.json` 键和 `CLAUDE.local.md` 区块 | 每个团队 1 个 | 私有 |

每台机器只拿基础层加上它被允许用的覆盖层。一条命令铺到本机，一条命令把本机改动写回去。

三种典型用法：

- **只有自己，几台机器**：一个基础层就够，覆盖层可以不要。
- **几个团队，自己的机器**：基础层加每个团队一个覆盖层，按项目远程地址自动套用。
- **团队发的机器**：基础层加这一个团队的覆盖层，别的团队的层不装。

## 2. 几个名词

- **层**：一个根目录带 `layer.toml` 的目录。可以是 git 仓库（cclayer clone 到本机），也可以就是本机的一个目录，比如网盘同步文件夹里的一个目录。
- **设备清单**：`~/.config/cclayer/device.toml`，记这台机器启用了哪些层、层在本机哪里、项目在哪里找。只在本机，不进任何仓库。
- **clone 目录**：git 层在本机的副本，默认 `~/.local/share/cclayer/<层名>`。目录层没有副本，就是那个目录本身。
- **项目根目录（roots）**：cclayer 在这些目录下找 git 仓库，看它们的远程地址归哪个覆盖层。
- **匹配**：覆盖层的 `[[match]] remote = "github.com/acme-inc/*"` 对上某个仓库的远程地址，这个仓库就归这个覆盖层。一个仓库最多归一个覆盖层。
- **注入**：apply 把覆盖层 `project/` 下的内容写进匹配项目的 `.claude/settings.local.json` 和 `CLAUDE.local.md`。这两个文件会登记进全局 git 排除列表，不会被提交进项目。
- **状态目录**：`~/.local/state/cclayer/`，记上次 apply 写了哪些文件的哈希、项目归属、已跑过的插件命令，还有被覆盖文件的备份。

cclayer 不碰的东西：Claude Code 的登录、会话记录、提示词历史、`~/.claude.json`、`projects/`，以及 `settings.json` 里的 `env`、`permissions.allow`、`permissions.defaultMode`、`permissions.ask`。这些永远留在本机。

## 3. 安装

macOS 用 Homebrew：

```sh
brew install --cask zhaojiannet/tap/cclayer
```

以后有新版本时用 `brew upgrade --cask cclayer` 升级。

Linux、Windows 和不用 Homebrew 的机器，去 [GitHub Releases](https://github.com/zhaojiannet/cclayer/releases) 下载对应平台的压缩包，解压后把 `cclayer` 放进 PATH。

还需要 git。插件、MCP 和 `leave` 里的 purge 这几步需要 Claude Code 2.1.288 以上。PATH 里找不到 `claude` 时，插件和 MCP 这两步会跳过并提示，`leave` 则在清理前停下。

装好后：

```sh
cclayer version
cclayer help
```

## 4. 最简单的用法：一个人，几台机器，用目录同步

不想碰 git 的话，层就是一个目录。把它放在网盘的同步文件夹里，几台机器填同一个路径。

### 第一台机器

```sh
cclayer setup
```

setup 是一个全屏界面。左栏上面是「层」，列出这台设备用的基础层和覆盖层；下面是「这台设备」，放项目目录、自动拉取这类只属于本机的设置。右栏显示左边选中那一项的说明和可改的字段，还会说明保存时会做什么，比如克隆到哪里、在哪里写入起步文件。底部一行写着保存前还差什么，旁边是「保存并应用」「只保存」「退出」三个按钮。打开时光标已经停在第一项要填的字段上。上下键选择，Enter 修改，Tab 在左栏、右栏和按钮之间切换，Esc 返回，按 `?` 看全部按键。按下保存之前什么都不写。界面语言跟随系统，也可以在「语言」一行换成 English、简体中文或日本語。

这台机器只要改两行：

1. **基础层**：填一个目录，比如 `~/Dropbox/cclayer/base`。目录还不存在，保存时会写入起步的 `layer.toml`。
2. **项目目录**：你放代码的目录，逗号分隔，比如 `~/Projects`。

其余几行保持默认。自动拉取默认关：打开后，等第 10 节的 SessionStart hook 放进基础层的 `claude/settings.json`，每次开 Claude Code 会话会先自动 `apply`（目录层没有拉取这一步，只做 apply）。profiles 先关着，第 9 节再说。受信任的层不选。最后选「保存并应用」，apply 完会自动跑 `doctor`。

起步的 `layer.toml` 长这样：

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

`paths` 是层要管的 `~/.claude` 下的文件和目录，`settings_keys` 是层要管的 `settings.json` 里的键。只有你自己用、目录也只在你的网盘里时，把 `private = true` 那行的注释去掉，`check` 就不再拦你写在规则里的邮箱。这时层里还是空的，所以 apply 什么都没写。

接下来把本机已有的配置收进层：

```sh
cclayer capture
```

它会列出「只在本机有、层里没有」的文件，并告诉你用 `--add` 收进去：

```sh
cclayer capture --add CLAUDE.md --add rules/ --add skills/
```

路径相对 `~/.claude`。写之前每个文件都过一遍 `check`：带密钥形状的字串、邮箱、指向你家目录的绝对路径这些会被拒绝并指出在哪一行，改掉再收。`settings.json` 不用 `--add`，`settings_keys` 列出的键只要本机改过就会写回。

然后看一眼：

```sh
cclayer status
ls ~/Dropbox/cclayer/base/claude
```

等网盘把目录同步完。

### 第二台机器

装 cclayer，等网盘把 `~/Dropbox/cclayer/base` 同步下来，然后：

```sh
cclayer setup
```

基础层地址填同一个路径。这次目录里已经有 `layer.toml`，setup 直接用它。选「保存并应用」后，本机的 `~/.claude` 就有了第一台机器的配置。本机已有、内容不同的同名文件算冲突：apply 会列出来问你要不要覆盖，同意后先把旧版备份到 `~/.local/state/cclayer/backups/` 再覆盖。

### 以后

- 哪台机器改了 `~/.claude` 下的东西：`cclayer capture`，等同步。
- 另一台机器：`cclayer apply`。开了自动拉取、并按第 10 节放好 hook 的话，开 Claude Code 会话时自动做。
- 两边都改了同一个文件：`apply` 会把它列为冲突，交互式运行时问你要不要用层里的版本覆盖本机，自动模式不碰。

注意：网盘同步的是普通文件，没有版本历史，两台机器同时改同一个文件时以网盘的处理为准。要历史和合并，用第 5 节的 git 方式。

## 5. 用 git 仓库同步

### 建基础层仓库

最省事的办法是先按第 4 节在本机建一个目录层，收好内容，再把这个目录变成仓库：

```sh
cd ~/cclayer-base            # setup 时填的目录
git init -b main
git add -A
git commit -m "base layer"
git remote add origin git@github.com:you/cclayer-base.git
git push -u origin main
```

基础层里没有身份信息，可以是公开仓库；私有也行。

也可以先在 GitHub 建仓库、手工写 `layer.toml` 和 `claude/` 目录推上去，再在机器上 `setup` 填仓库地址。空仓库不行，`setup` 要在里面找到 `layer.toml`。

### 其它机器

```sh
cclayer setup
```

基础层地址填仓库地址，`https://github.com/you/cclayer-base.git` 或 `git@github.com:you/cclayer-base.git`。公开仓库的话，连接方式选「不用配」。cclayer 把它 clone 到 `~/.local/share/cclayer/base`。

### 日常

```sh
cclayer apply --pull         # 先把 clone 快进到远程最新，再铺到本机
cclayer capture              # 本机改动写回 clone，不提交
cd ~/.local/share/cclayer/base
git diff                     # 看一下
git add -A && git commit -m "..." && git push
```

`capture` 故意不提交不推送，让你先看 diff。`status` 会显示每个 clone 有没有未提交的改动、比远程多几个提交。

## 6. 加一个团队：覆盖层

每个团队一个私有仓库（或目录）。内容：

```
layer.toml
project/settings.local.json    写进匹配项目 .claude/settings.local.json 的键
project/CLAUDE.local.md        写进匹配项目 CLAUDE.local.md 的受管区块
git/fragment.gitconfig         可选，这个团队的 hook 和 alias，只在匹配的仓库里生效
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

逐段解释：

- `[identity]`：在匹配的仓库里 git 用这个名字和邮箱提交。cclayer 用 `includeIf "hasconfig:remote.*.url:..."` 让它只在这些仓库里生效，不会碰别的仓库。
- `[[match]]`：远程地址模式，主机和组织两段必须写明，不能用通配符，两个覆盖层也不能写同一个主机加组织。之后的部分 `*` 匹配一段，`**` 匹配多段。可以写多条。`https://`、`git@`、`ssh://` 三种写法的远程都能对上，大小写不同也能对上。
- `[inject] settings_keys`：`project/settings.local.json` 里哪些键写进项目。`permissions.allow` 永远不动，项目自己点过的允许项保留。
- `[inject] claude_local`：这个文件的内容写进项目 `CLAUDE.local.md` 的 `<!-- cclayer:begin -->` 和 `<!-- cclayer:end -->` 之间，区块外的内容不动。
- `[git] fragment`：原始 git 配置片段。不能有 `[user]`（身份走 `[identity]`）、远程地址和 `include`。可以放 `pull.rebase`、`push.default`、`merge.conflictstyle`、`diff.algorithm` 这类逐个列出的常见设置；其他键，包括 alias，以及 `core.hooksPath`、`credential.helper` 这类能让 git 执行程序的，这一层必须列在设备清单的 `trust_exec` 里，否则 apply 拒绝。

覆盖层不能有 `[claude]` 段，它从不往 `~/.claude/` 写东西。

在 setup 里加覆盖层时填一个不存在的目录，setup 会接着问提交者姓名、邮箱、远程模式，保存时生成上面这套起步文件，只是不带 `[git]` 段和片段文件，团队需要时再自己加。

### 加到机器上

```sh
cclayer layer add acme git@github.com:you/cclayer-acme.git
```

名字用团队短名（小写字母、数字、连字符）。地址是仓库时会先问用部署密钥还是令牌，详见第 7 节；也可以直接加 `--method deploy-key` 或 `--method token`。公开仓库，或 git 已经能访问时，加 `--method none` 跳过这个问题。地址是目录时，里面必须已经有 `layer.toml`，起步文件只有 setup 会写。它配好凭证、clone、检查这一层能不能用、补上 blocklist，任何一步失败都把设备清单恢复原样。之后跑 `cclayer apply`。

也可以重跑 `cclayer setup`，在「层」里选「+ 添加覆盖层」。

或者直接改 `~/.config/cclayer/device.toml`：

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

然后 `cclayer init` clone 缺的层，`cclayer apply` 铺上。

### 验证

```sh
cclayer status                   # 每个覆盖层下面列出归它的项目
cd ~/Projects/acme-api
git config --get user.email      # 应该是 me@acme.example
cat .claude/settings.local.json
cat CLAUDE.local.md
cclayer doctor                   # 检查身份是否对、include 块是否在位、排除列表是否齐
```

没有覆盖层匹配的仓库里，`default_identity` 留空时 git 会拒绝提交并要你设身份，这是故意的，防止用错身份。想让这类仓库默认用某一层的身份，在 setup 的「默认身份」一行选它，或在清单里写 `default_identity = "personal"`。

### 多个团队

每个团队再加一层即可。`layers` 的顺序就是应用顺序，后面的优先，但一个项目只会归一个覆盖层；两个覆盖层的模式都对上同一个仓库时，apply 把它列为冲突并要你改模式。

## 7. 私有层仓库的凭证

团队发的机器上，一般只有这个团队给的 git 凭证，够不到你的私有层仓库。`keys setup` 给两条路，每个层单独选：

### Deploy key（自动）

```sh
cclayer keys setup acme --method deploy-key
```

cclayer 在本机生成一把 ed25519 钥匙 `~/.ssh/cclayer-acme`，往 `~/.ssh/config` 开头加一段受管的 `Host cclayer-acme`，把层地址改成走这个别名。如果 `gh` 已登录并且你是这个仓库的管理员，它用 `gh api` 把公钥挂到仓库上并给写权限；否则把公钥和要跑的命令打印出来，你在有权限的地方跑。最后用 `git ls-remote` 验证能连上。

一把 deploy key 只能打开一个仓库，不绑任何账号，机器丢了去仓库设置里删掉这把钥匙就行。网络封了 22 端口加 `--port-443`，走 `ssh.github.com:443`。

### HTTPS token（手动建）

```sh
cclayer keys setup acme --method token
```

cclayer 打印建 token 的步骤：打开 GitHub 的 fine-grained token 页面，资源所有者选仓库所属的账号或组织，仓库访问选「只选这些仓库」并勾上层仓库，权限 Contents 读写。建好后粘贴给 cclayer。它先用这个 token 验证一次（不经过任何凭证助手），仓库明确拒绝就不存；通过后存进 git 的凭证助手，只对这个仓库路径生效，不写进任何文件。

你不是仓库管理员、或者 SSH 被封时用这条。

### 查看和删除

```sh
cclayer keys list
cclayer keys remove acme     # 删钥匙和 ssh 配置段，或告诉你怎么丢掉 token；打印要在 GitHub 上吊销什么
```

setup 里给仓库选连接方式，做的就是这件事。目录层不需要凭证，setup 不会问。

## 8. 日常命令详解

### apply

```sh
cclayer apply [--pull] [--mcp-force] [--replay-plugins]
cclayer apply --hook
```

按顺序做这些事：

1. 加锁，防止两个 apply 同时跑。
2. `--pull` 时先把每个 git 层的 clone 快进到远程。有未提交改动、没有上游分支、不是快进、离线，都跳过并说明；目录层说明「nothing to pull」。
3. 对每个层跑 `check`，拒绝不该进层的内容。
4. 把基础层 `claude/` 下 `paths` 列出的文件镜像进 `~/.claude`。它记得上次写了什么：本机改过、层里也变了的文件算冲突，交互式问你要不要覆盖，`--hook` 不碰并点名。本机删掉的文件算本机改动：层没变就不重写，层也变了算冲突。被覆盖的文件先备份。
5. 合并 `settings.json`：只动 `settings_keys` 列出的键，别的键原样保留。外观、模型、`attribution`、`permissions.deny` 这类已知无害的键直接合并。只做限制的键，层保持或增加限制时直接合并，删除或放宽限制时要确认；层只是让某个键保持关闭（本机没设或本来就关着，层也设成 `false`）时直接合并，设成 `false` 反而放宽限制的开关除外；其他键，特别是会让 Claude Code 执行程序的（`hooks`、`statusLine`、`apiKeyHelper` 这类助手命令键、`extraKnownMarketplaces`、`enabledPlugins`），有变化时先展示再确认；镜像的文件里，`hooks/`、`skills/`、`commands/`、`agents/` 下的文件、非 markdown 文件、带执行位或 shebang 的文件，有变化也要先列出内容再确认。
6. 生成 `~/.gitconfig.cclayer` 和每个覆盖层一个的 `~/.gitconfig.cclayer.d/<层>.gitconfig`，在 `~/.gitconfig` 末尾维护一个带标记的 include 块，其余内容不动。这个块按 include 的路径识别，标记注释丢了也能认出来替换掉，不会再加一份。
7. 插件和 marketplace：按基础层清单列出要跑的 `claude plugin` 命令，确认一次后执行。每台机器只跑一次，`--replay-plugins` 重跑。本机已登记的市场和 user 范围已装的插件无论如何都跳过，因为重新登记市场会冲掉 `autoUpdate` 这类设置。覆盖层的插件以 `--scope local` 装进匹配的项目。
8. MCP server：基础层 `mcp/<名>.json` 每个文件一个 server，`claude mcp get` 不认识的才添加，`--mcp-force` 删了重加。密钥不写明文，写成 `${VAR}` 这样的环境变量引用。
9. 对每个匹配项目写注入文件，登记全局 git 排除列表。覆盖层要写进项目的这类键同样先展示再确认。不再匹配的项目把注入的内容撤掉。
10. 开了 profiles 的话，把基础层复制进每个 profile 目录。

`--hook` 是给 SessionStart hook 用的非交互模式：不问任何问题，需要确认的事一律跳过并列出来。

### capture

```sh
cclayer capture [--add <相对 ~/.claude 的路径>]... [--from <项目目录>]
```

apply 的反方向。本机 `~/.claude` 下、层管的那些路径里，和上次 apply 写的不一样的文件，写回所属层。只在本机有的新文件列出来不动，`--add` 收进去，可以写单个文件，也可以写目录（收进其下所有新文件）。`settings.json` 里 `settings_keys` 的键改过就写回。

匹配项目里的 `.claude/settings.local.json` 和 `CLAUDE.local.md` 改了也写回覆盖层；几个项目改得不一致时拒绝，`--from` 指定以哪个项目为准。

每个文件先过 `check`，被拒的不写并报出原因。写完只是改了层目录里的文件，git 层要你自己提交推送。

### check

```sh
cclayer check
```

检查每个层目录。所有层都拒绝的内容：

- 密钥形状的字串（各种 token、`KEY=` 和 JSON 里的密钥值）
- git 片段里的 `[user]`、远程地址
- Claude Code 的运行时文件、`projects/`

基础层另外拒绝：

- 邮箱地址（`example.com` 这类示例域除外）。基础层 `layer.toml` 声明了 `private = true` 时不查这一条
- 设备清单 `blocklist` 里的词。`blocklist` 由 `init`/`setup` 从覆盖层自动填：层名、提交者姓名、邮箱的本地部分和域、远程模式里的组织名。这样团队名不会混进公开的基础层。单个字符不收。某个词确实该出现在基础层（比如你自己公开的插件市场所在的 GitHub 用户名），把它写进设备清单的 `blocklist_except`，`init` 不会把它加进 blocklist，`check` 也不拦
- `[includeIf]`、指向你家目录的绝对路径

apply 和 capture 都会自动跑它。

### status

每个层一行：目录、是不是本地目录、有没有未提交改动、比远程多或少几个提交。然后按覆盖层列出归它的项目，再列没匹配到的和冲突的。

### doctor

检查已知的坑并给出修法：`~/.claude` 是不是 git 仓库、有没有符号链接、`CLAUDE_CONFIG_DIR` 设得对不对、`~/.gitconfig` 的 include 块在不在末尾、全局排除列表齐不齐、每个匹配项目的 git 身份对不对、`claude` 在不在 PATH。开了 profiles 时还查每个 profile 目录里有没有符号链接、有没有 `CLAUDE.md`（`~/.claude/CLAUDE.md` 本来就会加载，profile 里再放一份会加载两次）。

### leave

```sh
cclayer leave acme [--force]
```

不再给某个团队干活时用。先列出要清理的项目让你确认，然后：对每个项目跑 `claude purge` 清掉 Claude Code 的项目状态、删 profile 目录、撤掉注入的文件、删这一层的凭证、去掉这一层重新生成 git 配置、保存清单，最后删 clone。clone 有未提交或未推送的改动时拒绝，`--force` 才删。层是你自己指定的目录时不删，原地保留。结束时列出还要手工做的事，比如去 GitHub 吊销凭证。

### init

读你写好的 `device.toml`，clone 缺的层，填 `blocklist`。只问一句要不要开自动拉取。没有 `device.toml` 时，它用逐行提问问出各层地址、项目目录和默认身份，再写出清单。之后自己跑 `apply`。

## 9. Profiles：每个团队一套独立的 Claude Code 目录

默认所有项目共用 `~/.claude`，登录、会话记录、提示词历史也是一套。想按团队分开，设备清单里 `profiles = true`（setup 里也能开）。之后 apply 把基础层复制进 `~/.claude-profiles/<层>/`，每个覆盖层一份，`CLAUDE.md` 除外，Claude Code 总会从 `~/.claude` 读它。

启动 Claude Code 时让它用对应的目录：

```sh
cd ~/Projects/acme-api
cclayer env                 # 打印 export CLAUDE_CONFIG_DIR=...，给 direnv 或 shell 用
eval "$(cclayer env)"
cclayer run -- claude       # 或者直接带着变量启动
```

`profile_dir` 可以改默认位置。`leave` 时对应的 profile 目录一起删。

## 10. SessionStart hook：开会话自动 apply

把这段放进基础层的 `claude/settings.json`，`hooks` 要在 `settings_keys` 里：

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

设备清单里 `auto_pull = true` 时，每次开 Claude Code 会话先 `apply --hook`；为 `false` 时这个 hook 什么都不做。没装 cclayer 的机器上 hook 直接通过。

## 11. 文件在哪

| 路径 | 是什么 |
|---|---|
| `~/.config/cclayer/device.toml` | 设备清单，只在本机 |
| `~/.local/share/cclayer/<层>/` | git 层的 clone |
| `~/.local/state/cclayer/` | 上次 apply 的记录、锁 |
| `~/.local/state/cclayer/backups/` | 被覆盖文件的备份，每次运行一个目录，留最近 5 次加首次 |
| `~/.gitconfig.cclayer` | cclayer 生成的 git 主配置 |
| `~/.gitconfig.cclayer.d/<层>.gitconfig` | 每个覆盖层的身份和片段 |
| `~/.gitconfig` 末尾的标记块 | 唯一被 cclayer 改动的地方 |
| `~/.ssh/cclayer-<层>`、`~/.ssh/config` 开头的标记块 | deploy key 方式的钥匙和 ssh 别名 |
| `~/.claude-profiles/<层>/` | profiles 模式的配置目录 |

环境变量 `CCLAYER_DEVICE` 和 `CCLAYER_STATE` 可以改清单和状态目录的位置。`CCLAYER_LANG` 选界面语言（`en`、`zh`、`ja`），不设时先看设备清单里的 `lang`，再看系统语言。报错信息始终是英文，方便搜索。

## 12. 常见问题

**apply 说某个文件是冲突。** 本机和层里都改了它。交互式运行会问要不要用层的版本覆盖本机；想保留本机的就答否，然后 `capture` 写回层。

**apply 拒绝了 git 片段，提示 trust_exec。** 片段里有 `core.hooksPath` 这类能让 git 执行程序的键，或者 cclayer 不认识的键。确认这一层是你自己控制的，再把层名加进设备清单的 `trust_exec`。

**capture 把文件拒了。** 看它报的规则。密钥换成 `${VAR}` 从环境变量读；邮箱放进覆盖层的 `[identity]`；团队名不该出现在基础层。

**没有覆盖层匹配的仓库里 git 不让提交。** `default_identity` 留空时这是故意的。要么给这个仓库加一个覆盖层并写上匹配模式，要么设 `default_identity`。

**status 说 `local directory`。** 这个层是目录层，不走 git。想要历史和合并，按第 5 节把目录推成一个仓库，再跑 `cclayer setup` 把这一层的地址改成仓库地址，由 cclayer 自己 clone。目录层里即使有 `.git`，cclayer 也不在里面运行 git：那份 `.git/config` 不是 cclayer 写的，里面的设置可能让 git 执行程序。

**网盘同步的目录两边同时改了。** 以网盘的处理为准，cclayer 不做合并。要合并就用 git。

**Windows。** 没有 Homebrew，从 Releases 下载。在 Windows 上 `cclayer env` 打印 PowerShell 语法。

**setup 里填错了。** 再选那一项重填就行，保存前什么都没写。本次加错的覆盖层，在右栏按「删除这个覆盖层」。已经保存过的覆盖层要去掉，用 `cclayer leave <层>`。

**setup 在管道或脚本里跑。** 加 `--accessible`，改成一行一问的纯文本模式；标准输入不是终端时自动切换。
