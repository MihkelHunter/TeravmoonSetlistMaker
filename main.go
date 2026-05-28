package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"time"

	"TeravmoonSetlistMaker/spotify"
)

//go:embed header.txt
var header string

type Song struct {
	Title              string
	Album              string
	Length             time.Duration
	SpotifyPlayedCount int
	SpotifyPopularity  int
	PopularityModifyer float64
	EnergyLevel        int    // 1–10
	Tuning             string // Drop D, Standard, etc.
	IsCloser           bool
	Role               string
	Score              float64
}

type Candidate struct {
	Song  Song
	Score float64
}

const (
	Opener   = "opener"
	Anthem   = "anthem"
	Breather = "breather"
	Closer   = "closer"
	Encore   = "encore"
	Filler   = "filler"
)

func NewSong(
	title string,
	album string,
	min int,
	sec int,
	energy int,
	playedCount int,

	tuning string,
	role string,
) Song {
	return Song{
		Title: title,
		Length: time.Duration(min)*time.Minute +
			time.Duration(sec)*time.Second,

		EnergyLevel: energy,
		Tuning:      tuning,
		Role:        role,

		SpotifyPlayedCount: playedCount,
		PopularityModifyer: 1,
	}
}

func score(candidate Song, targetEnergy int, previous Song) float64 {
	score := 0.0
	var albumMulti float64 = 1
	// energy fit
	score += math.Abs(float64(candidate.EnergyLevel-targetEnergy)) * 4

	// tuning penalty
	if previous.Title != "" && previous.Tuning != candidate.Tuning {
		score += 5
	}

	if candidate.Album == "Loodan, et Sul Pole Paha Meel" && candidate.Role == Filler {
		albumMulti = 2
	}

	// popularity bonus
	if candidate.SpotifyPopularity > 0 {
		score -= float64(candidate.SpotifyPopularity) / 30 * albumMulti
	} else {
		score -= float64(candidate.SpotifyPlayedCount) / 3000 * albumMulti
	}

	return score
}

func PickWeighted(cands []Candidate) Song {
	if len(cands) == 1 {
		return cands[0].Song
	}

	totalWeight := 0.0
	weights := make([]float64, len(cands))

	for i, c := range cands {

		// lower score = stronger weight
		w := 1.0 / (c.Score + 1)

		weights[i] = w
		totalWeight += w
	}

	r := rand.Float64() * totalWeight

	running := 0.0

	for i, w := range weights {
		running += w

		if r <= running {
			return cands[i].Song
		}
	}

	return cands[0].Song
}

func GenerateSet(
	songs []Song,
	targetDuration time.Duration,
	variability float64, // 0.0 - 1.0
) []Song {
	var set []Song
	used := map[string]bool{}

	total := time.Duration(0)

	energyCurve := []int{
		8, 7, 8, 5, 6, 7, 8,
	}

	var prev Song

	// ---- opener ----

	var openers []Candidate

	for _, s := range songs {

		if s.Role != Opener {
			continue
		}

		openers = append(
			openers,
			Candidate{
				Song: s,
				Score: score(
					s,
					8,
					prev,
				),
			},
		)
	}

	sort.Slice(openers,
		func(i, j int) bool {
			return openers[i].Score <
				openers[j].Score
		})

	topN := max(
		2,
		int(
			2+variability*4,
		),
	)

	if len(openers) > topN {
		openers = openers[:topN]
	}

	opener := PickWeighted(openers)

	set = append(set, opener)
	used[opener.Title] = true
	total += opener.Length
	prev = opener

	// ---- fill set ----

	curveIndex := 0

	for total < targetDuration-(8*time.Minute) {

		targetEnergy := energyCurve[curveIndex%
			len(energyCurve)]

		var candidates []Candidate

		for _, s := range songs {

			if used[s.Title] {
				continue
			}

			if s.Role == Closer ||
				s.Role == Encore {
				continue
			}

			candidates = append(
				candidates,
				Candidate{
					Song: s,
					Score: score(
						s,
						targetEnergy,
						prev,
					),
				},
			)
		}

		if len(candidates) == 0 {
			break
		}

		sort.Slice(
			candidates,
			func(i, j int) bool {
				return candidates[i].Score <
					candidates[j].Score
			},
		)

		topN = max(
			3,
			int(
				3+variability*5,
			),
		)

		if len(candidates) > topN {
			candidates = candidates[:topN]
		}

		chosen := PickWeighted(
			candidates,
		)

		set = append(
			set,
			chosen,
		)

		used[chosen.Title] = true
		total += chosen.Length
		prev = chosen

		curveIndex++
	}

	// ---- closer ----

	var closers []Candidate

	for _, s := range songs {

		if used[s.Title] {
			continue
		}

		if s.Role != Closer {
			continue
		}

		closers = append(
			closers,
			Candidate{
				Song: s,
				Score: score(
					s,
					10,
					prev,
				),
			},
		)
	}

	if len(closers) > 0 {

		sort.Slice(
			closers,
			func(i, j int) bool {
				return closers[i].Score <
					closers[j].Score
			},
		)

		if len(closers) > 2 {
			closers = closers[:2]
		}

		closer := PickWeighted(
			closers,
		)

		set = append(set, closer)
		total += closer.Length
	}

	// ---- encore maybe ----

	for _, s := range songs {
		if s.Role == Encore &&
			total+s.Length <= targetDuration {

			if rand.Float64() < .6 {
				set = append(
					set,
					s,
				)
			}

			break
		}
	}

	return set
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func main() {
	length := flag.Int("duration", 50, "setlist duration")
	variability := flag.Float64("variability", 0.65, "setlist variability")
	useSpotify := flag.Bool("spotify", false, "fetch song data from Spotify API, default value is false")

	flag.Parse()

	songs := []Song{
		NewSong("Raha Seina Sees", "Tee", 4, 10, 3, 1134, "Drop D", Anthem),
		NewSong("Narr", "Tee", 4, 35, 5, 582, "Standard", Breather),
		NewSong("Vedur", "Tee", 4, 9, 8, 4098, "Standard", Opener),
		NewSong("Olla või Minna", "Tee", 3, 50, 3, 764, "Standard", Breather),
		NewSong("Kodukäija", "Tee", 3, 24, 7, 5552, "Standard", Anthem),
		NewSong("Paanika", "Tee", 4, 27, 9, 397, "Drop D", Closer),
		NewSong("Kordan Kordan Korrutan", "Tee", 4, 38, 6, 296, "Standard", Anthem),
		NewSong("Maasikas", "Tee", 4, 3, 6, 1369, "Standard", Anthem),
		NewSong("Kuhu", "Tee", 3, 0, 5, 5589, "Standard", Anthem),
		NewSong("Äratus", "Tee", 3, 41, 9, 275, "Drop D", Anthem),
		NewSong("Ajupesumasin", "Tee", 3, 31, 9, 3791, "Drop D", Anthem),
		NewSong("Peremees", "Tee", 2, 59, 9, 1786, "Drop D", Opener),
		NewSong("Tee", "Tee", 8, 39, 6, 312, "Standard", Closer),
		NewSong("Probleemid ja Pelmeenid", "Tee", 3, 26, 6, 5663, "Standard", Anthem),
		NewSong("Suured Mootorid", "Loodan, et Sul Pole Paha Meel", 3, 40, 5, 6857, "Standard", Opener),
		NewSong("Varjude Mäng", "Loodan, et Sul Pole Paha Meel", 4, 38, 7, 5846, "Standard", Opener),
		NewSong("Apokalüpsilehmad", "Loodan, et Sul Pole Paha Meel", 3, 35, 8, 4030, "Standard", Encore),
		NewSong("Voolukaamel", "Loodan, et Sul Pole Paha Meel", 3, 21, 2, 3478, "Standard", Filler),
		NewSong("Tõmbame Sae Käima", "Loodan, et Sul Pole Paha Meel", 3, 55, 6, 6857, "Standard", Closer),
		NewSong("Põrgurattur", "Loodan, et Sul Pole Paha Meel", 3, 3, 5, 5800, "Standard", Encore),
		NewSong("Kiirus", "Loodan, et Sul Pole Paha Meel", 3, 7, 5, 3061, "Standard", Filler),
		NewSong("Haige", "Loodan, et Sul Pole Paha Meel", 3, 14, 3, 10384, "Standard", Anthem),
		NewSong("Kondibluus", "Loodan, et Sul Pole Paha Meel", 3, 52, 1, 5820, "Standard", Filler),
		NewSong("Hevikopter", "Loodan, et Sul Pole Paha Meel", 3, 51, 8, 3904, "Drop D", Filler),
		NewSong("Magmapagan", "Loodan, et Sul Pole Paha Meel", 3, 56, 4, 2255, "Standard", Filler),
	}

	// ---- optional Spotify overlay ----

	if *useSpotify {
		clientID := getEnv("SPOTIFY_CLIENT_ID", "")
		clientSecret := getEnv("SPOTIFY_CLIENT_SECRET", "")
		if clientID == "" || clientSecret == "" || clientID == "your_client_id_here" || clientSecret == "your_client_secret_here" {
			log.Fatal("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET must be set in .env or environment")
		}

		client := spotify.NewClient(clientID, clientSecret)

		for i, s := range songs {
			info, err := client.SearchTrack("Teravmoon", s.Title)
			if err != nil {
				fmt.Printf("  [!] Spotify search failed for %q: %v\n", s.Title, err)
				continue
			}
			if info == nil {
				fmt.Printf("  [!] %q not found on Spotify\n", s.Title)
				continue
			}
			songs[i].Title = info.Name
			songs[i].Album = info.Album
			songs[i].Length = time.Duration(info.DurationMs) * time.Millisecond
			songs[i].SpotifyPopularity = info.Popularity
			// TODO: There could be multiple songs in spotify with the same title and artist. I want to sum them.
		}
	}

	// ---- generate setlist ----

	set := GenerateSet(
		songs,
		time.Duration(*length)*time.Minute,
		*variability, // variability
	)
	var total time.Duration

	fmt.Print("\n\n\n\n")
	fmt.Println(header)
	fmt.Println("Setlist length:", *length)
	fmt.Println("Variability:", *variability)

	fmt.Printf("\nSETLIST\n")

	for i, s := range set {
		pop := ""
		if s.SpotifyPopularity > 0 {
			pop = fmt.Sprintf(" Pop:%d", s.SpotifyPopularity)
		}
		fmt.Printf(
			"%-3d | %-30s | %-10s | Energy:%2d%s\n",
			i+1,
			s.Title,
			s.Tuning,
			s.EnergyLevel,
			pop,
		)

		total += s.Length
	}

	fmt.Println()
	fmt.Println("Runtime:", total)
}
