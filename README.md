# Discord Go Bot (Termux 运行)

这是一个基于 Go + Discordgo 的聊天机器人示例，具备：
- 基础对话能力（调用 OpenAI 格式兼容接口）
- 自动对话摘要（防止上下文过长）
- 简单 RAG 检索（使用 embedding 进行相似度召回）

## 功能概览
- **聊天**：通过 `OPENAI_CHAT_MODEL` 调用聊天模型。
- **自动总结**：当对话条数超过阈值时生成摘要并保留关键信息。
- **RAG 检索**：对历史用户消息生成 embedding，召回相似内容辅助回答。

## 环境变量
| 变量 | 说明 |
| --- | --- |
| `DISCORD_TOKEN` | Discord 机器人 Token（必填） |
| `OPENAI_API_KEY` | OpenAI 兼容 API Key（必填） |
| `OPENAI_BASE_URL` | OpenAI 兼容 API Base URL（默认 `https://api.openai.com/v1`） |
| `OPENAI_CHAT_MODEL` | 聊天模型（默认 `gpt-4o-mini`） |
| `OPENAI_EMBED_MODEL` | embedding 模型（默认 `text-embedding-3-small`） |
| `SYSTEM_PROMPT` | 系统提示词（可选） |

## Termux 运行
1. 安装依赖：
   ```bash
   pkg update
   pkg install golang git
   ```
2. 拉取代码并进入目录：
   ```bash
   git clone <你的仓库地址>
   cd discord-
   ```
3. 设置环境变量：
   ```bash
   export DISCORD_TOKEN="你的discord bot token"
   export OPENAI_API_KEY="你的openai兼容key"
   export OPENAI_BASE_URL="https://api.openai.com/v1"
   ```
4. 启动：
   ```bash
   go run ./cmd/discordbot
   ```

## 注意事项
- 机器人需要在 Discord 开发者后台开启 **Message Content Intent**。
- 在 Termux 中长时间运行建议使用 `tmux`。
