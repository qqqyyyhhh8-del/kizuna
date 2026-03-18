# Discord Go Bot

[简体中文](README.md) | [English](README.en.md) | [插件编写指南](PLUGIN_GUIDE.md)

当前版本：`v0.6.1`  
更新记录见 [CHANGELOG.md](CHANGELOG.md)

这是一个基于 Go + Discordgo 的聊天机器人示例，具备：
- 基础对话能力（调用 OpenAI 格式兼容接口）
- 自动对话摘要（防止上下文过长）
- 简单 RAG 检索（使用 embedding 召回，可选 rerank 重排）
- SQLite + sqlite-vec 持久化存储（聊天记忆 / 世界书 / 插件状态）
- 插件化的人设 / 服务器表情 / 主动回复扩展能力
- 允许发言范围、管理员、system prompt 的核心 Slash 管理能力
- 外部插件宿主与 Git 安装式插件生态
- 世界书注入与服务器表情世界书持久化

## 功能概览
- **聊天**：通过 `OPENAI_CHAT_MODEL` 调用聊天模型。
- **触发规则**：私聊会直接回复，群聊中在 `@机器人` 或直接回复机器人消息时回复，避免刷屏。
- **上下文**：按频道维度保留上下文，不按用户隔离；模型能看到每条消息的用户 ID、用户名、昵称、显示名、UTC+8 时间，以及回复目标消息的显式元数据。
- **多模态输入**：用户消息中的服务器自定义表情会转成图片一起发给聊天模型；图片附件也会作为图片输入发送。
- **自动总结**：当对话条数超过阈值时生成摘要并保留关键信息。
- **RAG 检索**：对历史用户消息生成 embedding，召回后可选再走 rerank 重排。
- **官方插件**：`/persona`、`/emoji`、`/proactive` 已迁移到官方插件仓库 `discord-bot-plugins`，按需安装后即可注册对应 Slash 命令。
- **允许发言范围**：机器人默认不会在任何服务器、频道、子区发言；管理员需要在目标位置直接执行 `/setup` 打开管理面板，并按当前上下文放行服务器、频道或子区。
- **插件生态**：支持从 Git 仓库安装外部进程插件；插件通过 JSON-RPC over stdio 接入，可注册自己的 slash 命令、处理消息、追加 prompt、改写回复，并使用受控能力访问宿主。

## 官方插件
- 官方插件仓库：[`qqqyyyhhh8-del/discord-bot-plugins`](https://github.com/qqqyyyhhh8-del/discord-bot-plugins)
- 在 `/plugin` 面板点击 `安装`，然后填写：
  人设管理：`repo=https://github.com/qqqyyyhhh8-del/discord-bot-plugins.git`，`path=plugins/persona`
  主动回复：`repo=https://github.com/qqqyyyhhh8-del/discord-bot-plugins.git`，`path=plugins/proactive`
  服务器表情管理：`repo=https://github.com/qqqyyyhhh8-del/discord-bot-plugins.git`，`path=plugins/emoji`

安装后会自动注册对应的 `/persona`、`/proactive`、`/emoji` 命令。

## 环境变量
| 变量 | 说明 |
| --- | --- |
| `DISCORD_TOKEN` | Discord 机器人 Token（必填）；也兼容读取 `DISCORDTOKEN`、`DISCORD_BOT_TOKEN`、`discordtoken` |
| `OPENAI_API_KEY` | OpenAI 兼容 API Key（必填） |
| `OPENAI_BASE_URL` | OpenAI 兼容 API Base URL（默认 `https://api.openai.com/v1`） |
| `OPENAI_CHAT_MODEL` | 聊天模型（默认 `gpt-4o-mini`） |
| `OPENAI_EMBED_BASE_URL` | embedding 专用 OpenAI 兼容 Base URL；不填则沿用 `OPENAI_BASE_URL` |
| `OPENAI_EMBED_API_KEY` | embedding 专用 API Key；不填则沿用 `OPENAI_API_KEY` |
| `OPENAI_EMBED_MODEL` | embedding 模型（默认 `text-embedding-3-small`） |
| `OPENAI_RERANK_BASE_URL` | rerank 专用 OpenAI 兼容 Base URL；不填则沿用 `OPENAI_BASE_URL` |
| `OPENAI_RERANK_API_KEY` | rerank 专用 API Key；不填则沿用 `OPENAI_API_KEY` |
| `OPENAI_RERANK_MODEL` | rerank 模型；为空则关闭 rerank |
| `OPENAI_HTTP_TIMEOUT_SECONDS` | 可选，给 OpenAI 兼容接口设置 HTTP 客户端超时秒数；不填则主要依赖外层 context 超时 |
| `SYSTEM_PROMPT` | 系统提示词（可选） |
| `BOT_SQLITE_PATH` | 主 SQLite 数据库路径（默认 `bot.db`）；运行时配置、插件注册/存储、聊天记忆、摘要和 sqlite-vec 检索索引都保存在这里 |
| `BOT_CONFIG_FILE` | 兼容导入/管理员引导配置文件（默认 `bot_config.json`）；旧版字段会在首次启动时迁移到 SQLite，`super_admin_ids` 仍从这里读取 |
| `BOT_COMMAND_GUILD_ID` | 可选，slash 命令注册到指定 guild；不填则注册为全局命令 |
| `PLUGINS_DIR` | 插件宿主工作目录（默认 `plugins`），其中会保存已安装插件源码、仓库缓存和临时目录 |

## 快速开始
1. 拉取仓库并进入目录：
   ```bash
   git clone <你的仓库地址>
   cd discord-
   ```
2. 创建 `.env`：
   ```bash
   cp .env.example .env
   ```
3. 按需编辑 `.env`。
4. 启动：
   ```bash
   go run ./cmd/discordbot
   ```

程序启动时会自动读取当前目录下的 `.env`。如果你已经在系统环境里设置了同名变量，系统环境优先。

## Docker 部署
仓库已提供 [Dockerfile](Dockerfile) 和 [docker-compose.yml](docker-compose.yml)。

1. 复制环境变量文件：
   ```bash
   cp .env.example .env
   ```
2. 编辑 `.env`，至少填好 `DISCORD_TOKEN`、`OPENAI_API_KEY` 等必需项。
3. 准备管理员引导配置文件：
   ```bash
   mkdir -p data
   cat > data/bot_config.json <<'EOF'
   {
     "super_admin_ids": ["你的Discord用户ID"],
     "admin_ids": []
   }
   EOF
   ```
4. 启动容器：
   ```bash
   docker compose up -d --build
   ```

说明：
- 容器内默认使用 `/data/bot.db`、`/data/bot_config.json`、`/data/plugins`。
- `./data` 会挂载到容器内 `/data`，用于长期保存 SQLite 数据库、管理员引导配置和插件目录。
- 如果你是从旧目录迁移到 Docker，请把旧的 `bot_config.json` 放到 `data/bot_config.json`，把旧的 `plugins/registry.json` 放到 `data/plugins/registry.json`，首次启动会自动导入。
- 机器人不需要暴露 HTTP 端口。
- 由于插件安装/升级依赖 `git`，插件运行默认使用 `go run`，镜像里已保留 `git` 和 `go`。

## 存储与兼容导入
运行时数据现在默认持久化到 `BOT_SQLITE_PATH` 指向的 SQLite 数据库（默认 `bot.db`），包括：
- 频道上下文、摘要、sqlite-vec 检索向量
- 额外 system prompt、允许发言范围、世界书、服务器表情分析结果
- 插件注册表、插件私有存储

`BOT_CONFIG_FILE` 现在主要承担两个作用：
- 首次启动时兼容导入旧版 `bot_config.json`
- 持续作为超级管理员 / 静态管理员的引导配置文件

如果 `BOT_CONFIG_FILE` 不存在，程序会自动生成一个模板：

```json
{
  "super_admin_ids": ["你的Discord用户ID"],
  "admin_ids": [],
  "system_prompt": "",
  "speech_mode": "allowlist",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": [],
  "worldbook_entries": {}
}
```

- `super_admin_ids` 只能手动编辑配置文件；支持直接写字符串 ID，也支持写数字形式的 Discord ID。
- `admin_ids` 可以手动编辑，也可以由超级管理员通过 slash 命令增删。
- 旧版 `system_prompt`、`speech_mode`、`allowed_*`、`worldbook_entries`、`guild_emoji_profiles` 等字段会在数据库尚未初始化时自动迁移到 SQLite。
- `system_prompt` 用来保存额外的 system prompt，例如你说的“破限配置”。
- `speech_mode` 当前默认固定为 `allowlist`；机器人只有命中允许范围时才会发言。
- `allowed_guild_ids` 是允许发言的服务器 ID 列表。
- `allowed_channel_ids` 是允许发言的频道 ID 列表。
- `allowed_thread_ids` 是允许发言的子区/线程/帖子 ID 列表。
- `worldbook_entries` 用来保存世界书条目；目前服务器表情分析结果会自动写到这里。
- 旧版本里可能出现 `personas`、`active_persona`、`proactive_reply`、`proactive_chance` 这些字段；它们属于迁移前的兼容数据，官方插件现在使用插件私有存储。

## Slash 命令
- `/help`：查看命令帮助
- `/setup`：打开允许发言范围管理面板
- `/plugin`：打开一站式插件管理面板
- `/system show`：查看额外 system prompt
- `/system set prompt:<prompt>`：设置额外 system prompt
- `/system clear`：清空额外 system prompt
- `/admin list`：查看超级管理员和管理员列表
- `/admin add user:<@user>`：超级管理员添加管理员
- `/admin remove user:<@user>`：超级管理员移除管理员

## 注意事项
- 机器人需要在 Discord 开发者后台开启 **Message Content Intent**。
- 群聊里请使用 `@机器人 你的问题`，或直接回复机器人上一条消息来触发回复。
- 机器人在收到触发消息后，会在生成回复的过程中持续显示 `typing`。
- 首次启动后机器人默认不会在任何群聊位置发言；请先在目标位置使用 `/setup` 面板配置允许范围。
- 升级到当前版本时，如目录里还存在旧的 `bot_config.json` 或 `plugins/registry.json`，启动后会自动迁移进 SQLite。
- 管理配置改为 slash commands，不再使用 `!persona`、`!system`、`!admin` 这类消息前缀命令。
- `/persona`、`/emoji`、`/proactive` 现在由官方插件提供；未安装对应插件时，这些命令不会出现在机器人里。
- 如果官方 `/emoji` 插件分析时遇到超时，优先检查你的 OpenAI 兼容站点响应速度；必要时可在 `.env` 里设置 `OPENAI_HTTP_TIMEOUT_SECONDS=600`。
- 机器人启动时会先清空当前作用域下旧的 slash commands，再批量重新注册，避免逐个删命令带来的额外请求。

## 插件开发

- 插件编写指南：[PLUGIN_GUIDE.md](PLUGIN_GUIDE.md)
- 示例插件导航：
  [examples/plugins/style-note/plugin.json](examples/plugins/style-note/plugin.json) ·
  [examples/plugins/style-note/cmd/style-note-plugin/main.go](examples/plugins/style-note/cmd/style-note-plugin/main.go) ·
  [pkg/pluginapi/types.go](pkg/pluginapi/types.go) ·
  [pkg/pluginapi/sdk.go](pkg/pluginapi/sdk.go)
- 插件协议与 SDK 在 `pkg/pluginapi`。
- 插件 manifest 文件固定为 `plugin.json`。
- 宿主当前支持：slash 命令、按钮/Modal 前缀路由、消息事件、prompt 注入、回复后处理、定时任务、插件私有存储、受控宿主能力调用。
- 官方样例插件在 `examples/plugins/style-note`。
- 安装当前仓库内的样例插件：
  在 `/plugin` 面板点击 `安装`，填写 `repo=https://github.com/qqqyyyhhh8-del/discord-.git`，`path=examples/plugins/style-note`

## 许可证

本项目使用 [MIT License](LICENSE)。
