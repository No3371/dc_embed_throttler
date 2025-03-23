# Discord Embed Throttler Bot

A Discord bot that helps manage embeds in channels by automatically suppressing them and allowing users to restore them with a limit.

## Features

- Automatically suppresses embeds in configured channels
- Allows message authors to restore embeds via right-click menu
- Tracks restore count per user per channel
- Configurable restore limit per user
- Channel-specific toggle for embed throttling (requires Manage Channels permission)

## Prerequisites

- Go 1.23.2 or later
- SQLite3
- Discord Bot Token with appropriate permissions

## Setup

1. Clone this repository
2. Copy `config/config.yaml` to `config.yaml` in the root directory
3. Edit `config.yaml` and add your bot token:
   ```yaml
   token: "your-bot-token-here"
   default_restore_limit: 3
   default_enabled: false
   database_path: "bot.db"
   ```
4. Run `go mod tidy` to install dependencies
5. Run `go run main.go` to start the bot

## Configuration Options

- `token`: Your Discord bot token
- `default_restore_limit`: Maximum number of times a user can restore embeds per channel
- `default_enabled`: Whether embed throttling is enabled by default for all channels
- `database_path`: Path to the SQLite database file

## Usage

### For Users
- When a message has its embeds suppressed, right-click the message and use the "Restore Embeds" option
- You can only restore embeds for your own messages
- You have a limited number of restores per channel (configured in `config.yaml`)

### For Channel Managers
- Right-click any message in the channel
- Use the "Toggle Embed Throttling" option to enable/disable embed throttling for the channel
- Requires the Manage Channels permission

## Database Schema

The bot uses SQLite to store:
- Restore counts per user per channel
- Channel-specific embed throttling settings

## Contributing

Feel free to submit issues and enhancement requests! 