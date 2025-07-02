package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
)

type Config struct {
	SpotifyClientID     string `json:"spotify_client_id"`
	SpotifyClientSecret string `json:"spotify_client_secret"`
	BookmarkPath        string `json:"bookmark_path"`
	BookmarkPosition    int    `json:"bookmark_position"`
	MusicDirectory      string `json:"music_directory"`
	TitlePosition       int    `json:"title_position"`
	ArtistPosition      int    `json:"artist_position"`
	MusicSeparator      string `json:"music_separator"`
	FFmpegPath          string `json:"ffmpeg_path"`
	YtDlpPath           string `json:"yt_dlp_path"`
	MaxConcurrent       int    `json:"max_concurrent"`
	AudioQuality        string `json:"audio_quality"`
}

func defaultConfig() Config {
	homeDir, _ := os.UserHomeDir()
	var bookmarkPath string

	switch runtime.GOOS {
	case "windows":
		bookmarkPath = filepath.Join(homeDir, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Bookmarks")
	case "darwin":
		bookmarkPath = filepath.Join(homeDir, "Library", "Application Support", "Google", "Chrome", "Default", "Bookmarks")
	default:
		bookmarkPath = filepath.Join(homeDir, ".config", "google-chrome", "Default", "Bookmarks")
	}

	return Config{
		SpotifyClientID:     "",
		SpotifyClientSecret: "",
		BookmarkPath:        bookmarkPath,
		BookmarkPosition:    0,
		MusicDirectory:      "./downloaded_music",
		TitlePosition:       1,
		ArtistPosition:      0,
		MusicSeparator:      " - ",
		FFmpegPath:          "ffmpeg", // Will be embedded
		YtDlpPath:           "yt-dlp", // Will be auto-downloaded
		MaxConcurrent:       3,
		AudioQuality:        "192k",
	}
}

type Track struct {
	URL    string `json:"url"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type SpotifySearchResponse struct {
	Tracks struct {
		Items []struct {
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Name        string `json:"name"`
				ReleaseDate string `json:"release_date"`
				Images      []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"album"`
		} `json:"items"`
	} `json:"tracks"`
}

type SpotifyTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type TrackMetadata struct {
	Title    string
	Artist   string
	Album    string
	Year     string
	CoverURL string
}

type BookmarkNode struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	URL      string         `json:"url"`
	Children []BookmarkNode `json:"children"`
}

type Bookmarks struct {
	Roots struct {
		BookmarkBar struct {
			Children []BookmarkNode `json:"children"`
		} `json:"bookmark_bar"`
	} `json:"roots"`
}

type MusicDownloader struct {
	config       Config
	spotifyToken string
	client       *http.Client
}

func NewMusicDownloader(config Config) *MusicDownloader {
	return &MusicDownloader{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (md *MusicDownloader) setupDependencies() error {
	color.Cyan("🔧 Setting up dependencies...")

	if err := os.MkdirAll(md.config.MusicDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create music directory: %v", err)
	}

	if err := md.updateYtDlp(); err != nil {
		color.Yellow("⚠️  Warning: Could not update yt-dlp: %v", err)
	}

	if err := md.setupFFmpeg(); err != nil {
		return fmt.Errorf("failed to setup FFmpeg: %v", err)
	}

	return nil
}

func (md *MusicDownloader) updateYtDlp() error {
	color.Blue("📥 Downloading/updating yt-dlp...")

	var url string
	var filename string

	switch runtime.GOOS {
	case "windows":
		url = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
		filename = "yt-dlp.exe"
	default:
		url = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp"
		filename = "yt-dlp"
	}

	resp, err := md.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		os.Chmod(filename, 0755)
	}

	md.config.YtDlpPath = "./" + filename
	color.Green("✅ yt-dlp updated successfully!")

	return nil
}

func (md *MusicDownloader) setupFFmpeg() error {
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		color.Yellow("⚠️  FFmpeg not found in PATH. Please install FFmpeg or place it in the same directory.")
		return fmt.Errorf("FFmpeg not available")
	}
	color.Green("✅ FFmpeg found!")

	return nil
}

func (md *MusicDownloader) parseBookmarks() ([]Track, error) {
	data, err := os.ReadFile(md.config.BookmarkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bookmarks: %v", err)
	}

	var bookmarks Bookmarks
	if err := json.Unmarshal(data, &bookmarks); err != nil {
		return nil, fmt.Errorf("failed to parse bookmarks: %v", err)
	}

	if md.config.BookmarkPosition >= len(bookmarks.Roots.BookmarkBar.Children) {
		return nil, fmt.Errorf("bookmark position %d out of range", md.config.BookmarkPosition)
	}

	musicFolder := bookmarks.Roots.BookmarkBar.Children[md.config.BookmarkPosition]
	var tracks []Track

	for _, bookmark := range musicFolder.Children {
		if bookmark.Type == "url" {
			track := md.parseTrackName(bookmark.Name, bookmark.URL)
			tracks = append(tracks, track)
		}
	}

	return tracks, nil
}

func (md *MusicDownloader) parseTrackName(name, url string) Track {
	parts := strings.Split(name, md.config.MusicSeparator)

	var title, artist string
	if len(parts) > md.config.TitlePosition {
		title = strings.TrimSpace(parts[md.config.TitlePosition])
	}
	if len(parts) > md.config.ArtistPosition {
		artist = strings.TrimSpace(parts[md.config.ArtistPosition])
	}

	if title == "" {
		title = name
	}
	if artist == "" {
		artist = "Unknown Artist"
	}

	return Track{
		URL:    url,
		Name:   name,
		Title:  title,
		Artist: artist,
	}
}

func (md *MusicDownloader) getSpotifyToken() error {
	if md.config.SpotifyClientID == "" || md.config.SpotifyClientSecret == "" {
		return fmt.Errorf("Spotify credentials not configured")
	}

	data := "grant_type=client_credentials"
	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(data))
	if err != nil {
		return err
	}

	req.SetBasicAuth(md.config.SpotifyClientID, md.config.SpotifyClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := md.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var tokenResp SpotifyTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	md.spotifyToken = tokenResp.AccessToken
	return nil
}

func (md *MusicDownloader) getSpotifyMetadata(track Track) (*TrackMetadata, error) {
	if md.spotifyToken == "" {
		if err := md.getSpotifyToken(); err != nil {
			return nil, err
		}
	}

	query := fmt.Sprintf("track:\"%s\" artist:\"%s\"", track.Title, track.Artist)
	url := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=track&limit=1", url.QueryEscape(query))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+md.spotifyToken)

	resp, err := md.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResp SpotifySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	if len(searchResp.Tracks.Items) == 0 {
		return nil, fmt.Errorf("no results found")
	}

	item := searchResp.Tracks.Items[0]
	metadata := &TrackMetadata{
		Title:  item.Name,
		Artist: item.Artists[0].Name,
		Album:  item.Album.Name,
		Year:   item.Album.ReleaseDate[:4],
	}

	if len(item.Album.Images) > 0 {
		metadata.CoverURL = item.Album.Images[0].URL
	}

	return metadata, nil
}

func (md *MusicDownloader) downloadCoverImage(coverURL, tempPath string) error {
	if coverURL == "" {
		return fmt.Errorf("no cover URL provided")
	}

	resp, err := md.client.Get(coverURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download cover: status %d", resp.StatusCode)
	}

	file, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)

	return err
}

func (md *MusicDownloader) downloadTrack(track Track, bar *progressbar.ProgressBar) error {
	outputPath := filepath.Join(md.config.MusicDirectory, track.Name+".mp3")

	if _, err := os.Stat(outputPath); err == nil {
		bar.Describe(fmt.Sprintf("⏭️  Skipping existing: %s", track.Name))
		return nil
	}

	bar.Describe(fmt.Sprintf("📥 Downloading: %s", track.Name))

	cmd := exec.Command(md.config.YtDlpPath,
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", md.config.AudioQuality,
		"--output", filepath.Join(md.config.MusicDirectory, track.Name+".%(ext)s"),
		"--quiet",
		"--no-warnings",
		track.URL,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download failed: %v - %s", err, stderr.String())
	}

	if md.config.SpotifyClientID != "" {
		bar.Describe(fmt.Sprintf("🎵 Adding metadata: %s", track.Name))
		if metadata, err := md.getSpotifyMetadata(track); err == nil {
			md.addMetadataToFile(outputPath, metadata)
		}
	} else {
		color.Yellow("⚠️  Skipping metadata: Spotify credentials not configured")
	}

	return nil
}

func (md *MusicDownloader) addMetadataToFile(filepath string, metadata *TrackMetadata) error {
	var coverPath string
	var cleanupCover bool

	if metadata.CoverURL != "" {
		coverPath = filepath + ".cover.jpg"
		if err := md.downloadCoverImage(metadata.CoverURL, coverPath); err == nil {
			cleanupCover = true
		} else {
			color.Yellow("⚠️  Could not download cover for %s: %v", metadata.Title, err)
			coverPath = ""
		}
	}

	defer func() {
		if cleanupCover && coverPath != "" {
			os.Remove(coverPath)
		}
	}()

	outputPath := filepath + ".tmp"

	var args []string

	if coverPath != "" {
		args = []string{
			"-i", filepath,
			"-i", coverPath,
			"-map", "0:a",
			"-map", "1:0",
			"-c:a", "copy",
			"-c:v", "mjpeg",
			"-disposition:v:0", "attached_pic",
			"-metadata", fmt.Sprintf("title=%s", metadata.Title),
			"-metadata", fmt.Sprintf("artist=%s", metadata.Artist),
			"-metadata", fmt.Sprintf("album=%s", metadata.Album),
			"-metadata", fmt.Sprintf("date=%s", metadata.Year),
			"-id3v2_version", "3",
			"-write_id3v1", "1",
			"-f", "mp3",
			"-y",
			outputPath,
		}
	} else {
		args = []string{
			"-i", filepath,
			"-c", "copy",
			"-metadata", fmt.Sprintf("title=%s", metadata.Title),
			"-metadata", fmt.Sprintf("artist=%s", metadata.Artist),
			"-metadata", fmt.Sprintf("album=%s", metadata.Album),
			"-metadata", fmt.Sprintf("date=%s", metadata.Year),
			"-id3v2_version", "3",
			"-write_id3v1", "1",
			"-f", "mp3",
			"-y",
			outputPath,
		}
	}

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("FFmpeg failed: %v - %s", err, stderr.String())
	}

	if err := os.Remove(filepath); err != nil {
		return fmt.Errorf("could not remove original file: %v", err)
	}

	if err := os.Rename(outputPath, filepath); err != nil {
		return fmt.Errorf("could not rename temp file: %v", err)
	}

	return nil
}

func (md *MusicDownloader) ProcessTracks() error {
	if err := md.setupDependencies(); err != nil {
		return err
	}

	color.Cyan("📚 Parsing bookmarks...")
	tracks, err := md.parseBookmarks()
	if err != nil {
		return err
	}

	if len(tracks) == 0 {
		color.Yellow("⚠️  No tracks found in bookmarks")
		return nil
	}

	color.Green("🎵 Found %d tracks to download", len(tracks))

	mainBar := progressbar.NewOptions(len(tracks),
		progressbar.OptionSetDescription("📥 Overall Progress"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: "░",
			BarStart:      "║",
			BarEnd:        "║",
		}),
	)

	semaphore := make(chan struct{}, md.config.MaxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for i, track := range tracks {
		wg.Add(1)
		go func(track Track, index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			trackBar := progressbar.NewOptions(1,
				progressbar.OptionSetDescription(fmt.Sprintf("Track %d/%d", index+1, len(tracks))),
			)

			if err := md.downloadTrack(track, trackBar); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("failed to download '%s': %v", track.Name, err))
				mu.Unlock()
				color.Red("❌ Failed: %s", track.Name)
			} else {
				color.Green("✅ Completed: %s", track.Name)
			}

			mainBar.Add(1)
		}(track, i)
	}

	wg.Wait()

	if len(errors) > 0 {
		color.Red("\n❌ Some downloads failed:")
		for _, err := range errors {
			color.Red("  • %v", err)
		}
	}

	color.Green("\n🎉 Processing complete! Downloaded to: %s", md.config.MusicDirectory)

	return nil
}

func loadConfig() Config {
	config := defaultConfig()

	if data, err := os.ReadFile("config.json"); err == nil {
		json.Unmarshal(data, &config)
	}

	return config
}

func saveConfig(config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile("config.json", data, 0644)
}

func configureInteractively() Config {
	config := loadConfig()
	scanner := bufio.NewScanner(os.Stdin)

	color.Cyan("🔧 Music Downloader Configuration")
	color.White("Press Enter to keep current values in [brackets]\n")

	fmt.Printf("Spotify Client ID [%s]: ", config.SpotifyClientID)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		config.SpotifyClientID = input
	}

	fmt.Printf("Spotify Client Secret [%s]: ", config.SpotifyClientSecret)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		config.SpotifyClientSecret = input
	}

	fmt.Printf("Chrome Bookmark Path [%s]: ", config.BookmarkPath)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		config.BookmarkPath = input
	}

	fmt.Printf("Bookmark Folder Position [%d]: ", config.BookmarkPosition)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		if pos, err := strconv.Atoi(input); err == nil {
			config.BookmarkPosition = pos
		}
	}

	fmt.Printf("Download Directory [%s]: ", config.MusicDirectory)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		config.MusicDirectory = input
	}

	fmt.Printf("Music Separator [%s]: ", config.MusicSeparator)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		config.MusicSeparator = input
	}

	fmt.Printf("Title Position [%d]: ", config.TitlePosition)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		if pos, err := strconv.Atoi(input); err == nil {
			config.TitlePosition = pos
		}
	}

	fmt.Printf("Artist Position [%d]: ", config.ArtistPosition)
	scanner.Scan()
	if input := strings.TrimSpace(scanner.Text()); input != "" {
		if pos, err := strconv.Atoi(input); err == nil {
			config.ArtistPosition = pos
		}
	}

	// Save configuration
	if err := saveConfig(config); err != nil {
		color.Red("⚠️  Could not save configuration: %v", err)
	} else {
		color.Green("✅ Configuration saved to config.json")
	}

	return config
}

func main() {
	color.Cyan("🎵 Standalone Music Downloader v1.0\n")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "config", "configure", "setup":
			configureInteractively()
			return
		case "update":
			config := loadConfig()
			md := NewMusicDownloader(config)
			if err := md.updateYtDlp(); err != nil {
				color.Red("❌ Update failed: %v", err)
			}
			return
		case "help", "-h", "--help":
			fmt.Println("Usage:")
			fmt.Println("  music-downloader          - Start downloading")
			fmt.Println("  music-downloader config   - Configure settings")
			fmt.Println("  music-downloader update   - Update yt-dlp")
			fmt.Println("  music-downloader help     - Show this help")
			return
		}
	}

	config := loadConfig()

	if config.SpotifyClientID == "" || config.BookmarkPath == "" {
		color.Yellow("⚠️  Configuration needed. Running setup...")
		config = configureInteractively()
	}

	md := NewMusicDownloader(config)
	if err := md.ProcessTracks(); err != nil {
		color.Red("❌ Error: %v", err)
		os.Exit(1)
	}
}
