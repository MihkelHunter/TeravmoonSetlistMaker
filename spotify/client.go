package spotify

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type TrackInfo struct {
	ID         string
	Name       string
	Album      string
	DurationMs int
	Popularity int
}

type Client struct {
	clientID     string
	clientSecret string
	token        string
	expiresAt    time.Time
	mu           sync.Mutex
	httpClient   *http.Client
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.expiresAt) {
		return nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating auth request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding auth response: %w", err)
	}

	c.token = result.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return nil
}

func (c *Client) SearchTrack(artist, title string) (*TrackInfo, error) {
	if err := c.authenticate(); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("q", fmt.Sprintf("track:%s artist:%s", title, artist))
	q.Set("type", "track")
	q.Set("limit", "5")

	req, err := http.NewRequest("GET", "https://api.spotify.com/v1/search?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.authenticate(); err != nil {
			return nil, err
		}
		return c.SearchTrack(artist, title)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Tracks struct {
			Items []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				Popularity int    `json:"popularity"`
				DurationMs int    `json:"duration_ms"`
				Album      struct {
					Name      string `json:"name"`
					AlbumType string `json:"album_type"`
				} `json:"album"`
			} `json:"items"`
		} `json:"tracks"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	if len(result.Tracks.Items) == 0 {
		return nil, nil
	}

	// prefer album version over single or compilation
	item := result.Tracks.Items[0]
	for _, t := range result.Tracks.Items {
		if t.Album.AlbumType == "album" {
			item = t
			break
		}
	}

	return &TrackInfo{
		ID:         item.ID,
		Name:       item.Name,
		Album:      item.Album.Name,
		DurationMs: item.DurationMs,
		Popularity: item.Popularity,
	}, nil
}
