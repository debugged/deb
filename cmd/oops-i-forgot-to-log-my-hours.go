package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/spf13/cobra"
)

type Commit struct {
	Hash        string `json:"hash"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	Date        string `json:"date"` // ISO 8601 from git (%aI)
	Message     string `json:"message"`
}

type RepoResult struct {
	Path          string               `json:"path"`
	Name          string               `json:"name"`
	CommitsByDate map[string][]*Commit `json:"commits_by_date"`
}

// flags
var (
	flagFrom     string
	flagTo       string
	flagJSON     bool
	flagMaxDepth int
)

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorBold    = "\033[1m"
	ColorCyan    = "\033[36m"
	ColorYellow  = "\033[33m"
	ColorGreen   = "\033[32m"
	ColorMagenta = "\033[35m"
	ColorGray    = "\033[90m"
	ColorWhite   = "\033[97m"
)

// oopsIforgotToLogMyHoursCmd represents the oops-i-forgot-to-log-my-hours command
var oopsIforgotToLogMyHoursCmd = &cobra.Command{
	Use:   "oops-i-forgot-to-log-my-hours",
	Short: "🕒💥 Recover the hours you *swear* you worked but forgot to log",

	Long: `Forgot to log your hours *again*? 😅 You're in good company — it happens so 
often around here it's basically an office tradition.

This command scans your git repos, finds all commits you made in a given 
date range, and turns them into a clean, per-day summary. Perfect for those 
moments when your timesheet is due in 5 minutes and you're questioning your 
entire week. 🧠⚠️

Examples:

  # Reconstruct last week's “productivity”
  oops-i-forgot-to-log-my-hours --from 2025-02-01 --to 2025-02-07

  # Go deeper because your projects are scattered across your machine 🙃
  oops-i-forgot-to-log-my-hours --from 2025-02-01 --depth 4 ~/projects

  # Output JSON (for automation… or at least the appearance of it 🤓)
  oops-i-forgot-to-log-my-hours --from 2025-02-01 --json
`,
	Run: func(cmd *cobra.Command, args []string) {

		root := "."
		if len(args) > 0 {
			root = args[0]
		}

		if flagFrom == "" {
			flagFrom = promptForFromDate()
		}

		if flagTo == "" {
			flagTo = time.Now().Format("2006-01-02")
		}

		var results []RepoResult

		// collect all git repos from the provided paths
		var repos []string
		seen := make(map[string]bool)

		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Calculate depth
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			depth := len(splitPath(rel))
			if depth > flagMaxDepth {
				return filepath.SkipDir
			}

			if info.IsDir() && info.Name() == ".git" {
				repoPath := filepath.Dir(path)
				if !seen[repoPath] {
					seen[repoPath] = true
					repos = append(repos, repoPath)
				}
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			log.Fatalf("error walking path %s: %v", root, err)
		}

		if len(repos) == 0 {
			fmt.Println("(no git repos found)")
			return
		}

		// ----------------------------------
		// Fetch all repos in parallel
		// ----------------------------------
		var (
			mu    sync.Mutex // protects results
			wg    sync.WaitGroup
			errCh = make(chan error, len(repos))
			sem   = make(chan struct{}, 8) // limit to 8 repos in parallel; tune as you like
		)

		for _, repoPath := range repos {
			wg.Add(1)
			go func(repoPath string) {
				defer wg.Done()

				// acquire semaphore slot
				sem <- struct{}{}
				defer func() { <-sem }()

				repoName := filepath.Base(filepath.Clean(repoPath))

				email, err := getGitUserEmail(repoPath)
				if err != nil {
					log.Fatalf("could not detect git user email: %v", err)
				}

				branches, err := listBranches(repoPath)
				if err != nil {
					errCh <- fmt.Errorf("unable to list branches for repo %s: %w", repoPath, err)
					return
				}

				commitMap := map[string]*Commit{}

				for _, br := range branches {
					commits, err := fetchCommitsForBranch(repoPath, br, flagFrom, flagTo)
					if err != nil {
						errCh <- fmt.Errorf("error fetching commits for branch %s in repo %s: %w", br, repoPath, err)
						return
					}

					for _, c := range commits {
						if strings.EqualFold(c.AuthorEmail, email) {
							if _, exists := commitMap[c.Hash]; !exists {
								commitMap[c.Hash] = c
							}
						}
					}
				}

				commitsByDate := map[string][]*Commit{}

				for _, c := range commitMap {
					dateKey := extractDateKey(c.Date)
					if dateKey == "" {
						log.Printf("could not parse date %q for commit %s\n", c.Date, c.Hash)
						continue
					}

					commitsByDate[dateKey] = append(commitsByDate[dateKey], c)
				}

				for _, list := range commitsByDate {
					sort.Slice(list, func(i, j int) bool {
						return list[i].Date < list[j].Date
					})
				}

				// append to shared results
				mu.Lock()
				results = append(results, RepoResult{
					Path:          repoPath,
					Name:          repoName,
					CommitsByDate: commitsByDate,
				})
				mu.Unlock()
			}(repoPath)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			if err != nil {
				log.Fatalf("error: %v", err)
			}
		}

		if flagJSON {
			if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
				log.Fatalf("error encoding JSON: %v", err)
			}
			return
		}

		printPretty(results)
	},
}

func init() {
	rootCmd.AddCommand(oopsIforgotToLogMyHoursCmd)

	oopsIforgotToLogMyHoursCmd.Flags().StringVar(&flagFrom, "from", "", "Start date (YYYY-MM-DD)")
	oopsIforgotToLogMyHoursCmd.Flags().StringVar(&flagTo, "to", "", "End date (YYYY-MM-DD, defaults to today)")
	oopsIforgotToLogMyHoursCmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of pretty format")
	oopsIforgotToLogMyHoursCmd.Flags().IntVar(&flagMaxDepth, "depth", 5, "Maximum directory traversal depth when searching for git repos")
}

func getGitUserEmail(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "config", "user.email")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git user.email not set for repo %s", repoPath)
	}
	email := strings.TrimSpace(string(out))
	if email == "" {
		return "", fmt.Errorf("git user.email empty for repo %s", repoPath)
	}
	return email, nil
}

func listBranches(repoPath string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return []string{}, nil
	}
	return strings.Split(raw, "\n"), nil
}

func fetchCommitsForBranch(repoPath, branch, from, to string) ([]*Commit, error) {
	format := "%H%x1f%an%x1f%ae%x1f%aI%x1f%s%x1e"
	cmd := exec.Command("git", "-C", repoPath,
		"log", branch,
		"--since="+from,
		"--until="+to,
		"--pretty=format:"+format,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	records := strings.Split(raw, "\x1e")
	var commits []*Commit

	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}

		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}

		commits = append(commits, &Commit{
			Hash:        fields[0],
			AuthorName:  fields[1],
			AuthorEmail: fields[2],
			Date:        fields[3],
			Message:     fields[4],
		})
	}

	return commits, nil
}

func extractDateKey(iso string) string {
	if t, err := time.Parse(time.RFC3339, iso); err == nil {
		return t.Format("2006-01-02")
	}
	if len(iso) >= 10 {
		return iso[:10]
	}
	return ""
}

func splitPath(p string) []string {
	parts := []string{}
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		if dir == "" || dir == string(filepath.Separator) {
			break
		}
		p = strings.TrimSuffix(dir, string(filepath.Separator))
	}
	return parts
}

func promptForFromDate() string {
	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("error creating screen: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("error initializing screen: %v", err)
	}
	defer screen.Fini()

	today := time.Now().Truncate(24 * time.Hour)
	current := today
	input := current.Format("2006-01-02")
	message := ""

	redraw := func() {
		screen.Clear()

		title := " 💥 OOPS I FORGOT TO LOG MY HOURS 🤯 "
		border := strings.Repeat("=", len(title))

		textStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)

		headerStyle := tcell.StyleDefault.
			Foreground(tcell.ColorGreen).
			Bold(true)

		writeStyledLine(screen, 0, 0, border, headerStyle)
		writeStyledLine(screen, 0, 1, title, headerStyle)
		writeStyledLine(screen, 0, 2, border, headerStyle)

		// Start content a few lines below the header
		offset := 4

		writeStyledLine(screen, 0, offset+0, "Select start date (YYYY-MM-DD)", textStyle)
		writeStyledLine(screen, 0, offset+2, "  Up / Down          : move one day earlier/later", textStyle)
		writeStyledLine(screen, 0, offset+3, "  Shift + Up / Down  : move one week earlier/later", textStyle)
		writeStyledLine(screen, 0, offset+4, "  Type               : edit the date (YYYY-MM-DD)", textStyle)
		writeStyledLine(screen, 0, offset+5, "  Enter              : confirm", textStyle)
		writeStyledLine(screen, 0, offset+6, "  Esc                : reset to today", textStyle)
		writeStyledLine(screen, 0, offset+7, "  Ctrl+C             : cancel", textStyle)

		y := offset + 10

		// draw date in a different style (e.g. bold cyan)
		dateStyle := tcell.StyleDefault.
			Foreground(tcell.ColorGreen).
			Bold(true)

		// --- Human readable date + week number on next line ---
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(input)); err == nil {
			// e.g. "Monday 03 February 2025"
			human := t.Format("Monday 02 January 2006")
			year, week := t.ISOWeek()
			line := fmt.Sprintf("%s (week %d, %d)", human, week, year)

			humanStyle := tcell.StyleDefault.
				Foreground(tcell.ColorYellow)

			writeStyledLine(screen, 0, y-1, line, humanStyle)
		}

		// --- From date line with colored date ---
		label := "From date: "
		writeStyledLine(screen, len(label), y, input, dateStyle)

		// draw label in default textStyle
		writeStyledLine(screen, 0, y, label, textStyle)

		// --- REAL TERMINAL CURSOR ---
		cursorX := len(label) + len(input)
		cursorY := y
		screen.ShowCursor(cursorX, cursorY)

		// Optional error message (move it down one line because of the new human-readable line)
		if message != "" {
			writeStyledLine(screen, 0, y+3, message, textStyle)
		}

		screen.Show()
	}

	redraw()

	for {
		ev := screen.PollEvent()

		switch e := ev.(type) {

		case *tcell.EventKey:
			key := e.Key()
			r := e.Rune()
			mod := e.Modifiers()

			// --- Cancel ---
			if key == tcell.KeyCtrlC {
				screen.Fini()
				fmt.Println("Cancelled.")
				os.Exit(1)
			}

			// --- Esc ---
			if key == tcell.KeyEsc || key == tcell.KeyEscape {
				input = today.Format("2006-01-02")
				message = ""
				redraw()
				continue
			}

			// --- Enter ---
			if key == tcell.KeyEnter {
				text := strings.TrimSpace(input)
				t, err := time.Parse("2006-01-02", text)
				if err != nil {
					message = fmt.Sprintf("🤨 \"%s\" is not a valid date. Try YYYY-MM-DD.", text)
					redraw()
					continue
				}
				if t.After(today) {
					message = "🚀 Time travel detected! Pick today or earlier 😅"
					redraw()
					continue
				}
				return t.Format("2006-01-02")
			}

			// --- Arrow keys (day changes) ---
			if key == tcell.KeyUp {
				if mod&tcell.ModShift != 0 {
					current = current.AddDate(0, 0, -7)
				} else {
					current = current.AddDate(0, 0, -1)
				}
				if current.After(today) {
					current = today
				}
				input = current.Format("2006-01-02")
				message = ""
				redraw()
				continue
			}
			if key == tcell.KeyDown {
				if mod&tcell.ModShift != 0 {
					next := current.AddDate(0, 0, 7)
					if next.After(today) {
						next = today
					}
					current = next
				} else {
					next := current.AddDate(0, 0, 1)
					if next.After(today) {
						continue
					}
					current = next
				}
				input = current.Format("2006-01-02")
				message = ""
				redraw()
				continue
			}

			// --- Backspace ---
			if key == tcell.KeyBackspace || key == tcell.KeyBackspace2 {
				if len(input) > 0 {
					input = input[:len(input)-1]
				}
				message = ""
				redraw()
				continue
			}

			// --- Typed characters ---
			if r != 0 {
				input += string(r)
				message = ""
				redraw()
				continue
			}

		}
	}
}

// Helper to write text to tcell screen
func writeStyledLine(s tcell.Screen, x, y int, text string, style tcell.Style) {
	for i, c := range text {
		s.SetContent(x+i, y, c, nil, style)
	}
}

// parseCommitLocalClock parses only the date + time part (YYYY-MM-DDTHH:MM:SS)
// and ignores the timezone offset, so 14:18Z and 14:18+01:00 are treated the same
// for intra-day duration calculations.
func parseCommitLocalClock(iso string) (time.Time, bool) {
	// Need at least "2006-01-02T15:04:05" = 19 chars
	if len(iso) < 19 {
		return time.Time{}, false
	}
	local := iso[:19] // strip timezone, keep "YYYY-MM-DDTHH:MM:SS"
	t, err := time.Parse("2006-01-02T15:04:05", local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// printPretty
func printPretty(results []RepoResult) {
	type ChangelogEntry struct {
		DateKey  string
		RepoName string
		RepoPath string
		Commit   *Commit
	}

	// Collect all entries across repos
	dateMap := make(map[string][]ChangelogEntry)

	for _, repo := range results {
		for date, commits := range repo.CommitsByDate {
			for _, c := range commits {
				dateMap[date] = append(dateMap[date], ChangelogEntry{
					DateKey:  date,
					RepoName: repo.Name,
					RepoPath: repo.Path,
					Commit:   c,
				})
			}
		}
	}

	if len(dateMap) == 0 {
		fmt.Println("(no commits)")
		return
	}

	// Sort dates
	dates := make([]string, 0, len(dateMap))
	for d := range dateMap {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	for _, date := range dates {
		fmt.Printf("📅 %s%s%s %s\n\n",
			ColorBold, ColorCyan, date, ColorReset)

		entries := dateMap[date]

		// Sort by timestamp & repo name
		sort.Slice(entries, func(i, j int) bool {
			ci := entries[i].Commit
			cj := entries[j].Commit

			if ci.Date == cj.Date {
				if entries[i].RepoName == entries[j].RepoName {
					return ci.Hash < cj.Hash
				}
				return entries[i].RepoName < entries[j].RepoName
			}
			return ci.Date < cj.Date
		})

		//----------------------------------------------
		// Build blocks: consecutive entries with same RepoName
		// Duration for a block = time(last commit) - time(first commit)
		//----------------------------------------------
		type block struct {
			StartIdx int
			EndIdx   int
			RepoName string
			Duration time.Duration
		}

		var blocks []block

		if len(entries) > 0 {
			startIdx := 0
			currentRepo := entries[0].RepoName

			for i := 1; i < len(entries); i++ {
				if entries[i].RepoName != currentRepo {
					// close block [startIdx, i-1]
					blocks = append(blocks, block{
						StartIdx: startIdx,
						EndIdx:   i - 1,
						RepoName: currentRepo,
					})
					// start new block
					startIdx = i
					currentRepo = entries[i].RepoName
				}
			}
			// close final block
			blocks = append(blocks, block{
				StartIdx: startIdx,
				EndIdx:   len(entries) - 1,
				RepoName: currentRepo,
			})
		}

		// Compute duration per block: LAST - FIRST commit timestamp
		for i := range blocks {
			b := &blocks[i]

			startT, okStart := parseCommitLocalClock(entries[b.StartIdx].Commit.Date)
			endT, okEnd := parseCommitLocalClock(entries[b.EndIdx].Commit.Date)

			if okStart && okEnd {
				d := endT.Sub(startT)
				if d < 0 {
					d = -d
				}
				b.Duration = d
			} else {
				b.Duration = 0
			}
		}

		// Quick lookup: block ending index → block
		blockByEnd := make(map[int]block)
		for _, b := range blocks {
			blockByEnd[b.EndIdx] = b
		}

		//----------------------------------------------
		// For per-project totals: track first & last commit time per project for this day
		//----------------------------------------------
		perProjectFirst := make(map[string]time.Time)
		perProjectLast := make(map[string]time.Time)

		for _, e := range entries {
			t, ok := parseCommitLocalClock(e.Commit.Date)
			if !ok {
				continue
			}
			if _, exists := perProjectFirst[e.RepoName]; !exists {
				perProjectFirst[e.RepoName] = t
			}
			perProjectLast[e.RepoName] = t
		}

		//----------------------------------------------
		// Print commits with emojis, colors and spacing
		//----------------------------------------------
		for b := range blocks {
			block := blocks[b]
			fmt.Printf("  %s%s%s\n",
				ColorBold, block.RepoName, ColorReset,
			)

			for i := block.StartIdx; i <= block.EndIdx; i++ {
				e := entries[i]
				c := e.Commit

				commitTime, ok := parseCommitLocalClock(c.Date)
				timeStr := ""
				if ok {
					timeStr = commitTime.Format("15:04")
				}

				fmt.Printf("    %s%s%s %s%s%s%s\n",
					ColorGray, timeStr, ColorReset,
					ColorYellow, c.Message, ColorReset,
					fmt.Sprintf(" (%s)", c.Hash[:7]),
				)
			}
			fmt.Println()
		}
	}
}
