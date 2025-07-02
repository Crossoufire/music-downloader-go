## Music Downloader in Go
A simple standalone tool that downloads music from your Chrome/Brave bookmarks and adds metadata from Spotify.

## Features
- Downloads music from YouTube URLs stored in browser's bookmarks
- Automatically adds metadata (title, artist, album, year) from Spotify
- Embeds album cover art into MP3 files
- Concurrent downloads for faster processing
- Cross-platform support (Windows, macOS, Linux)

## Quick Start

#### Download Release

1. Download the latest `.exe` file this repo
2. Run `music-downloader.exe config` to set up your config
3. Run `music-downloader.exe` to start downloading

#### Build from Source

```bash
git clone https://github.com/Crossoufire/music-downloader-go.git
cd music-downloader-go
go build -o music-downloader-go.exe
```

## Requirements

- FFmpeg: Required for audio processing and metadata embedding
  - Windows: Download from [ffmpeg.org](https://ffmpeg.org/download.html)
  - macOS: `brew install ffmpeg`
  - Linux: `sudo apt install ffmpeg`

## Setup

#### Spotify API (Optional but Recommended)

To get metadata and album covers, you need Spotify API credentials:

1. Go to [Spotify Developer Dashboard](https://developer.spotify.com/dashboard)
2. Create a new app
3. Copy your Client ID and Client Secret

#### Chrome Bookmarks Setup

1. Create a folder in your browser bookmarks bar
2. Add YouTube music URLs to this folder
3. Name each bookmark like: `Artist - Song Title` or Song `Title - Artist`

#### Configuration

Run the command:

```bash
music-downloader.exe config
```

## Bookmark Naming

The app parses bookmark names using these settings:

#### Music Separator

The character(s) that separate artist and title in your bookmark names.

- Default: ` - ` (space-dash-space)
- Example: `Adele - Hello` or `Hello - Adele`

#### Title Position & Artist Position

Which part of the split name is the title/artist (starting from 0).
Examples:

| Bookmark Name    | Separator | Title Pos | Artist Pos | Result                          |
|------------------|-----------|-----------|------------|---------------------------------|
| Adele - Hello    | ` - `     | 1         | 0          | Title: "Hello", Artist: "Adele" |
| Hello - Adele    | ` - `     | 0         | 1          | Title: "Hello", Artist: "Adele" |
| Adele_Hello_2015 | `_`       | 1         | 0          | Title: "Hello", Artist: "Adele" |

## Usage

```bash
# Start downloading
music-downloader.exe

# Configure settings
music-downloader.exe config

# Update yt-dlp
music-downloader.exe update

# Show help
music-downloader.exe help
```

## Configuration Options
These are the configuration options in `config.json`

| Setting	              | Description                             | Default              |
|-----------------------|-----------------------------------------|----------------------|
| spotify_client_id     | 	Spotify API Client ID	                 | ""                   |
| spotify_client_secret | Spotify API Client Secret	              | ""                   |
| bookmark_path         | 	Path to Chrome bookmarks file          | 	Auto-detected       |
| bookmark_position     | 	Which bookmark folder to use (0-based) | 	0                   |
| music_directory       | 	Where to save downloaded files	        | "./downloaded_music" |
| music_separator       | 	Character(s) separating artist/title   | 	" - "               |
| title_position        | Position of title in bookmark name      | 	1                   |
| artist_position	      | Position of artist in bookmark name	    | 0                    |
| max_concurrent        | 	Number of simultaneous downloads       | 	3                   |
| audio_quality	        | Audio quality for downloads	            | "192k"               |


---
Made with ❤️ for music lovers who organize their playlists in their bookmarks (very niche)
