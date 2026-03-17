# Discord Go Bot

[简体中文](README.md) | [English](README.en.md)

This is a Discord bot built with Go + Discordgo. It includes:
- Basic chat via OpenAI-compatible APIs
- Automatic conversation summarization to reduce context growth
- Simple RAG retrieval with embeddings and optional rerank
- Slash-based management panels for personas, speech scope, and guild emojis
- Worldbook injection with persistent guild emoji summaries

## Features
- **Chat**: Uses `OPENAI_CHAT_MODEL` for the main conversation model.
- **Trigger rules**: Replies in DMs directly. In guilds, it only replies when mentioned or when a user replies to the bot.
- **Context model**: Context is stored per channel, not isolated per user. The model sees sender ID, username, nickname, display name, UTC+8 timestamp, and explicit reply metadata.
- **Multimodal input**: Custom guild emojis in user messages are converted into images and sent to the chat model. Image attachments are also included as image input.
- **Auto summary**: Generates summaries after the message count crosses a threshold.
- **RAG retrieval**: Embeds historical user messages, retrieves relevant items, and optionally reranks them.
- **Emoji management**: Admins can run incremental analysis or full rebuild from the `/emoji` panel. The resulting emoji usage summary is stored in the worldbook for future replies.

## Environment Variables
| Variable | Description |
| --- | --- |
| `DISCORD_TOKEN` | Discord bot token (required). Also accepts `DISCORDTOKEN`, `DISCORD_BOT_TOKEN`, and `discordtoken` |
| `OPENAI_API_KEY` | OpenAI-compatible API key (required) |
| `OPENAI_BASE_URL` | OpenAI-compatible base URL for chat (default: `https://api.openai.com/v1`) |
| `OPENAI_CHAT_MODEL` | Chat model (default: `gpt-4o-mini`) |
| `OPENAI_EMBED_BASE_URL` | Base URL for embeddings. Falls back to `OPENAI_BASE_URL` if empty |
| `OPENAI_EMBED_API_KEY` | API key for embeddings. Falls back to `OPENAI_API_KEY` if empty |
| `OPENAI_EMBED_MODEL` | Embedding model (default: `text-embedding-3-small`) |
| `OPENAI_RERANK_BASE_URL` | Base URL for rerank. Falls back to `OPENAI_BASE_URL` if empty |
| `OPENAI_RERANK_API_KEY` | API key for rerank. Falls back to `OPENAI_API_KEY` if empty |
| `OPENAI_RERANK_MODEL` | Rerank model. Leave empty to disable rerank |
| `OPENAI_HTTP_TIMEOUT_SECONDS` | Optional HTTP client timeout for OpenAI-compatible endpoints. If empty, the outer context timeout is used |
| `SYSTEM_PROMPT` | Optional base system prompt |
| `BOT_CONFIG_FILE` | Runtime config file path (default: `bot_config.json`) |
| `BOT_COMMAND_GUILD_ID` | Optional guild ID for slash command registration. If empty, commands are global |

## Quick Start
1. Clone the repo and enter the directory:
   ```bash
   git clone <your-repo-url>
   cd discord-
   ```
2. Create `.env`:
   ```bash
   cp .env.example .env
   ```
3. Edit `.env` as needed.
4. Start the bot:
   ```bash
   go run ./cmd/discordbot
   ```

The bot loads `.env` from the current directory automatically. Existing shell environment variables take precedence over `.env`.

## Config File
If `BOT_CONFIG_FILE` does not exist, it will be created automatically on startup:

```json
{
  "super_admin_ids": ["your_discord_user_id"],
  "admin_ids": [],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "speech_mode": "all",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": [],
  "worldbook_entries": {},
  "guild_emoji_profiles": {}
}
```

- `super_admin_ids` can only be edited in the config file. Both string IDs and numeric Discord IDs are accepted.
- `admin_ids` can be edited in the config file or granted/revoked by a super admin through slash commands.
- `personas` stores persona prompts.
- `system_prompt` stores extra system prompt content, such as jailbreak-style policy overrides.
- `speech_mode` controls where the bot is allowed to speak: `all`, `none`, or `allowlist`.
- `allowed_guild_ids` is the allowlist of guild IDs.
- `allowed_channel_ids` is the allowlist of channel IDs.
- `allowed_thread_ids` is the allowlist of thread/forum post IDs.
- `worldbook_entries` stores worldbook entries. Guild emoji analysis currently writes here automatically.
- `guild_emoji_profiles` stores analyzed emoji IDs, summaries, the last operator, and timestamps for each guild.

## Slash Commands
- `/help`: show command help
- `/persona`: open the all-in-one persona management panel
  The panel supports viewing, switching, creating/overwriting, editing the current persona, deleting the current persona, clearing the active persona, and interactive controls.
- `/speech`: open the speech scope management panel
  The panel supports switching between all / none / allowlist and editing allowed guild IDs, channel IDs, and thread IDs.
- `/emoji`: open the guild emoji management panel
  The panel supports incremental analysis, full rebuild, refresh, and worldbook preview. Emoji analysis batches emojis in groups of 16 and sends them to the model as 4x4 image sheets.
- `/system show`: show the extra system prompt
- `/system set prompt:<prompt>`: set the extra system prompt
- `/system clear`: clear the extra system prompt
- `/admin list`: show super admins and admins
- `/admin add user:<@user>`: grant admin to a user (super admin only)
- `/admin remove user:<@user>`: revoke admin from a user (super admin only)

## Notes
- Enable **Message Content Intent** in the Discord developer portal.
- In guilds, use `@bot your message` or reply directly to the bot to trigger a response.
- The bot shows `typing` while it is processing a reply.
- Management has been moved to slash commands; old message-prefix commands such as `!persona`, `!system`, and `!admin` are not used anymore.
- `/persona` opens as an ephemeral panel by default. Regular users can view it; admins and super admins can operate it.
- `/speech` opens as an ephemeral panel by default. Only admins and super admins can operate it.
- `/emoji` opens as an ephemeral panel by default. Only admins and super admins can trigger emoji analysis.
- If `/emoji` analysis times out, check the response speed of your OpenAI-compatible endpoint. If needed, set `OPENAI_HTTP_TIMEOUT_SECONDS=600` in `.env`.
- On startup, the bot clears old slash commands in the current scope before re-registering them in bulk.

## License

This project is licensed under the [MIT License](LICENSE).
