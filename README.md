<div align="center">

<a href="https://github.com/happyx-app/HappyX"><img src="https://raw.githubusercontent.com/happyx-app/HappyX/main/docs/images/logo.svg" alt="HappyX logo" width="72" height="72"></a>

# HappyX.Bot

The Discord bot of **[HappyX](https://github.com/happyx-app/HappyX)**, the Discord bot that asks your team how they
feel every working day and turns the answers into reports.

[![CI](https://github.com/happyx-app/HappyX.Bot/actions/workflows/ci.yml/badge.svg)](https://github.com/happyx-app/HappyX.Bot/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go&logoColor=white)](go.mod)
[![Discord](https://img.shields.io/badge/Discord-bot-5865F2?style=flat-square&logo=discord&logoColor=white)](#setting-up-the-discord-application)
[![Image](https://img.shields.io/badge/ghcr.io-happyx--bot-8b5cf6?style=flat-square&logo=docker&logoColor=white)](https://github.com/orgs/happyx-app/packages)

[HappyX](https://github.com/happyx-app/HappyX) ·
[Api](https://github.com/happyx-app/HappyX.Api) ·
**Bot** ·
[Web](https://github.com/happyx-app/HappyX.Web) ·
[Bruno](https://github.com/happyx-app/HappyX.Bruno)

</div>

> [!TIP]
> New here? Start with the [main HappyX README](https://github.com/happyx-app/HappyX): what HappyX
> does, screenshots, the architecture and how to self-host the whole stack.

Discord bot for HappyX. Every day it sends each member of a server a private message asking
how they feel (1-5, with an optional comment), and sends the server's admin a weekly or monthly
report. One bot serves any number of servers; their data is kept apart by
[HappyX.Api](https://github.com/happyx-app/HappyX.Api).

## How it works

- **Joining a server** registers it in the API with the server owner as admin, and registers every
  human member (bots are ignored). New members are added as they join; members who leave are removed.
  The bot also keeps the server's roles and each member's roles in the API, so the dashboard can
  give access to a role (Settings → Admins and permissions) and that access follows who has it.
  It also sends the server's text and announcement channels, marking where it may post, so the
  dashboard can offer them for the kudos channel.
- **Removing the bot** from a server stops surveys and reports and hides the server from the
  dashboard. The API keeps its data for 30 days in case the bot is added back, then deletes it.
- **Deleting a server on the dashboard** (Settings → Delete server from HappyX) deletes all its data
  at once; within a minute the bot leaves the server by itself.
- **Setup assistant**: when the bot joins a server it DMs the owner a two-page assistant (survey,
  then report and extras) where every select saves at once; `/happyx setup` shows it again. It can
  send a test survey that saves nothing and post a short introduction of HappyX in a channel.
- **Survey**: on the server's working days (Monday to Friday by default; `/happyx config working_days`
  offers common sets, the web any days), from the configured hour (default 13:00 Europe/Madrid) the bot DMs every active member
  a card (greeting, the anonymity notice, the server's icon) with five emoji buttons. After
  answering, the card shows their mood, and they can add a comment. Answering again the same day replaces the answer.
  A **Don't ask me again** button under the survey stops it for that server (same as `/happyx pause`).
  Admins can write their own survey text on the web (variables like `{{user.name}}`, `{{user.streak}}`
  or `{{date}}` are filled in per person). It goes out as written in place of the greeting; the rest
  of the card and the buttons are always added, and its mentions never ping anyone.
- **Report**: on Mondays (weekly) or the 1st (monthly), from the survey hour, the admin gets the
  last period by DM as an image (average, daily trend, distribution) plus the comments as text.
- **Kudos**: `/happyx kudos user:@someone message:...` thanks a teammate in public. **Give kudos** in
  Discord's right-click Apps menu does the same from a user or a message (to its author), asking for
  the text in a form; from a message, the card also links to it. The card (with the receiver's
  avatar, who gave it and the message) is posted
  in the kudos channel (`/happyx config kudos_channel` or the dashboard's settings) or, without one, where the command was used,
  and the receiver gets a DM linking to it. No kudos to bots or to yourself, and at most 10 a day each.
  Reports count them and quote the latest five; kudos keep their names even in anonymous servers,
  since they are public anyway.
- **«Who's who?» game** (off by default, `/happyx config games:True` or the web settings): on
  Monday, from the survey hour, every member taking part gets an icebreaker question by DM and
  answers it in a modal until Wednesday (they can also answer on the web). On Thursday they get a
  link to guess on the web who wrote each answer, if at least 3 answered; on Monday the web shows
  who was who and the ranking. The DM tells members their name comes out at the end of the week.
  The Thursday DM also has a **Propose a question** button (sent on its own when there weren't
  enough answers to guess): until Sunday each member can propose next week's question, and on
  Monday the API draws one of them. With no proposals, questions rotate weekly from `game.q.*` in
  the locales. Turned on mid-week, the game starts that same week until Wednesday.
- The API records each survey and report it has sent, so restarts never send twice. A send
  that reached nobody (the API or Discord failed) is tried again 5 minutes later, up to 3 tries.
- Surveys are **anonymous by default**: the admin sees aggregated results and comments, not who wrote them.
- **Languages**: each server has a language (English by default, set with `/happyx config language`).
  Surveys, answers and reports use it. Slash command names and replies follow each user's Discord
  language when the bot has it, and the server's language otherwise.

## Commands

| Command | Who | What |
| --- | --- | --- |
| `/happyx status` | everyone | settings and whether you receive the survey |
| `/happyx pause` / `/happyx resume` | everyone | stop / resume receiving the survey in this server |
| `/happyx kudos` | everyone | `user`, `message` (≤300): publicly thank a teammate |
| Apps › **Give kudos** | everyone | right-click a user or a message: the same kudos, text written in a form |
| `/happyx setup` | manage | step-by-step setup assistant, only visible to whoever runs it |
| `/happyx config` | manage | `anonymous`, `hour`, `timezone`, `frequency`, `language`, `admin`, `kudos_channel`, `working_days`, `games` |
| `/happyx report` | view | see the last completed period's report now, or the one in progress with `period:current` |

**Who**: *view* and *manage* are given to users or roles on the dashboard. The HappyX admin (who
receives the reports), the server's owner and its Discord Administrators can do everything; only
they can change `admin`.

Discord shows these names translated for users whose client is in a supported language.

## Adding a language

Every language is one file in [`internal/i18n/locales`](internal/i18n/locales), named by its code
(`en.json`, `pt-BR.json`…).

1. Copy `en.json` to `<code>.json` and translate the values. Keep `{placeholders}` as they are.
2. Set `_name` to the language's own name (shown in `/happyx config language`) and `_discord` to the
   [Discord locales](https://discord.com/developers/docs/reference#locales) that should use it
   (`[]` if Discord has none, as with Catalan).
3. Run `go test ./...`. It fails if a key or placeholder is missing or extra, or a command name isn't
   valid for Discord (lowercase, no spaces, up to 32 characters).

No code changes are needed; the new language appears in `/happyx config` on the next deploy.
Discord allows at most 25 choices, so past 25 languages the option will need autocomplete instead.

## Previewing messages

`go run ./cmd/preview -lang es -out preview.html` writes a page that shows every message the bot
sends (survey, kudos, replies, report with its image) styled like Discord, built with the bot's own
code and sample data. No Discord server or API needed.

## Setting up the Discord application

1. Create an application at https://discord.com/developers/applications and add a **Bot**.
2. Under **Bot > Privileged Gateway Intents**, enable **Server Members Intent** (needed to list members).
3. Copy the bot token into `DISCORD_TOKEN`.
4. Invite it to a server. The easiest way is the **Add to Discord** button on the dashboard's
   landing page (it needs `DISCORD_CLIENT_ID` set in the API). Or use **OAuth2 > URL Generator**,
   scopes `bot` and `applications.commands`. No extra permissions are needed for the survey and
   reports, which go by DM. To post kudos in a `kudos_channel`, the bot needs **View Channel**,
   **Send Messages** and **Embed Links** there; otherwise kudos are posted as the reply to the
   command, which needs no permissions. The server's owner becomes its HappyX admin; change it
   with `/happyx config admin` or on the dashboard.

Members need "Allow direct messages from server members" enabled (Discord's default) to receive the survey.

## Configuration

All from environment variables (see `.env.example`):

| Variable | Purpose |
| --- | --- |
| `DISCORD_TOKEN` | Bot token |
| `API_URL` | HappyX.Api base URL, e.g. `http://api:8080` inside docker compose |
| `API_KEY` | API key; put its SHA-256 hash in the API's `API_KEY_HASHES` |
| `WEB_URL` | Optional. The dashboard's public URL, e.g. `https://happyx.example.com`. The game links there to guess, and the survey links the privacy policy until someone first answers since its date; without it the Thursday DM only asks for next week's question and the survey has no policy link |

## Development

```sh
go test ./...
# contract test against a running API (writes test data; not production):
HAPPYX_API_URL=http://localhost:8083 HAPPYX_API_KEY=... go test ./internal/api/
```

To run everything, use the `docker-compose.yml` in [HappyX](https://github.com/happyx-app/HappyX).

## How it's built

HappyX is developed with AI assistance. Every change is reviewed and approved by the maintainer before it's merged.

## License

[AGPL-3.0](LICENSE). The report fonts in `internal/report/fonts` keep their own licence ([OFL](internal/report/fonts/OFL.txt)). Versions published before 30 October 2026 (up to commit [`4102634`](https://github.com/happyx-app/HappyX.Bot/tree/410263403f30fbdef67da2117a568129d30f84db)) remain available under the MIT License.
