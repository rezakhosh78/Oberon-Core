package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSOCKSAddress = "127.0.0.1:1717"
	defaultHTTPAddress  = "127.0.0.1:1718"
	tuiWidth            = 68
)

type pickerItem struct {
	category string
	title    string
	detail   string
}

func runTUI() error {
	restoreOutput := enableTerminalOutput()
	defer restoreOutput()
	restoreScreen := enterTUIScreen()
	defer restoreScreen()
	in := bufio.NewReader(os.Stdin)
	for {
		drawMainMenu()
		choice, err := readPrompt(in, "Select 1, 2, 3, or 4: ")
		if err != nil {
			return err
		}
		switch strings.TrimSpace(choice) {
		case "1":
			if err := tuiConnect(in); err != nil {
				if err == errElevationRelaunched {
					return err
				}
				showTUIMessage(in, "CONNECTION FAILED", "Oberon could not start this connection.", err.Error())
			}
		case "2":
			if err := tuiScan(in); err != nil {
				showTUIMessage(in, "SCAN FAILED", "The endpoint scan could not be completed.", err.Error())
			}
		case "3":
			if err := tuiCreateWarpKey(in); err != nil {
				showTUIMessage(in, "KEY UPDATE FAILED", "The new WARP key could not be applied.", err.Error())
			}
		case "4", "q", "Q":
			drawTUIPage("GOODBYE", "Oberon is closing.", "Exiting Oberon.")
			return nil
		default:
			drawTUIPage("INVALID OPTION", "Choose one of the actions below.", "Enter 1, 2, 3, or 4.")
			waitPrompt(in)
		}
	}
}

func drawMainMenu() {
	drawTUIFrame("MAIN MENU", "Choose an action", func(width int) {
		printTUIRow(width, "", "")
		printTUIOption(width, "01", "Connect Oberon", "AmneziaWG WARP client: create if needed, scan, connect", "32;1")
		printTUIRow(width, "", "")
		printTUIOption(width, "02", "Scan Endpoint & Manual Connection", "Scan a local profile and select an endpoint", "36;1")
		printTUIRow(width, "", "")
		printTUIOption(width, "03", "Create WARP Key & Apply", "Register a new key and apply it to a profile", "33;1")
		printTUIRow(width, "", "")
		printTUIOption(width, "04", "Exit", "Close Oberon", "2")
	})
}

func drawTUIFrame(title, subtitle string, body func(int)) {
	drawTUIFrameWithProxies(title, subtitle, defaultSOCKSAddress, defaultHTTPAddress, body)
}

func drawTUIFrameWithProxies(title, subtitle, socksAddress, httpAddress string, body func(int)) {
	drawTUIFrameWithStatus(title, subtitle, socksAddress, httpAddress, "", body)
}

func drawTUIFrameWithStatus(title, subtitle, socksAddress, httpAddress, status string, body func(int)) {
	border := strings.Repeat("─", tuiWidth+2)
	clearTerminalScreen()
	fmt.Printf("╭%s╮\n", border)
	printTUIHeader(tuiWidth)
	fmt.Printf("├%s┤\n", border)
	printTUIRow(tuiWidth, title, "1;36")
	if subtitle != "" {
		printTUIWrapped(tuiWidth, subtitle, "2")
	}
	fmt.Printf("├%s┤\n", border)
	if body != nil {
		body(tuiWidth)
	}
	fmt.Printf("├%s┤\n", border)
	printTUIRow(tuiWidth, "LOCAL PROXIES", "1;36")
	proxySummary := ""
	if socksAddress != "" {
		proxySummary = "SOCKS5  " + socksAddress
	}
	if httpAddress != "" {
		if proxySummary != "" {
			proxySummary += "     "
		}
		proxySummary += "HTTP  http://" + httpAddress
	}
	if proxySummary == "" {
		proxySummary = "No local proxies enabled"
	}
	printTUIWrapped(tuiWidth, proxySummary, "2")
	if status != "" {
		fmt.Printf("├%s┤\n", border)
		printTUIRow(tuiWidth, status, "1;32")
	}
	fmt.Printf("╰%s╯\n", border)
}

func drawTUIPage(title, subtitle string, lines ...string) {
	drawTUIFrame(title, subtitle, func(width int) {
		for _, line := range lines {
			printTUIWrapped(width, line, "37")
		}
	})
}

func showTUIMessage(in *bufio.Reader, title, subtitle, message string) {
	drawTUIPage(title, subtitle, message)
	waitPrompt(in)
}

func enterTUIScreen() func() {
	if !isInteractiveTerminal() {
		return func() {}
	}
	fmt.Print("\033[?1049h\033[2J\033[H")
	return func() { fmt.Print("\033[?1049l") }
}

func startTerminalSpinner(label string) func() {
	if !isInteractiveTerminal() {
		fmt.Fprintln(os.Stderr, label+"...")
		return func() {}
	}
	fmt.Fprint(os.Stdout, "\033[?25l")
	frames := []string{"◐", "◓", "◑", "◒"}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(90 * time.Millisecond)
		defer ticker.Stop()
		index := 0
		for {
			fmt.Fprintf(os.Stdout, "\r\033[2K%s %s", paintTerminalText("1;36", frames[index%len(frames)]), label)
			index++
			select {
			case <-done:
				fmt.Fprint(os.Stdout, "\r\033[2K\033[?25h")
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-finished
		})
	}
}

func isInteractiveTerminal() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	input, inputErr := os.Stdin.Stat()
	output, outputErr := os.Stdout.Stat()
	return inputErr == nil && outputErr == nil && input.Mode()&os.ModeCharDevice != 0 && output.Mode()&os.ModeCharDevice != 0
}

func clearTerminalScreen() {
	if isInteractiveTerminal() {
		fmt.Print("\033[2J\033[H")
	}
}

func paintTerminalText(code, value string) string {
	if os.Getenv("NO_COLOR") != "" || !isInteractiveTerminal() {
		return value
	}
	return "\033[" + code + "m" + value + "\033[0m"
}

func printTUIHeader(width int) {
	left, right := "OBERON", "v"+Version
	spaces := width - len(left) - len(right)
	if spaces < 1 {
		spaces = 1
	}
	fmt.Printf("│ %s%s%s │\n", paintTerminalText("1;36", left), strings.Repeat(" ", spaces), paintTerminalText("1;37", right))
	printTUIRow(width, "AMNEZIAWG WARP CLIENT", "2")
}

func printTUIOption(width int, number, title, description, color string) {
	printTUIRow(width, "["+number+"]  "+title, color)
	printTUIRow(width, "      "+description, "2")
}

func printTUIRow(width int, text, color string) {
	runes := []rune(text)
	if len(runes) > width {
		text = string(runes[:width])
	}
	padding := width - len([]rune(text))
	if padding < 0 {
		padding = 0
	}
	fmt.Printf("│ %s%s │\n", paintTerminalText(color, text), strings.Repeat(" ", padding))
}

func printTUIWrapped(width int, text, color string) {
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			printTUIRow(width, "", color)
			continue
		}
		line := ""
		for _, word := range words {
			if len([]rune(word)) > width {
				word = shortenTUIText(word, width)
			}
			if line == "" {
				line = word
			} else if len([]rune(line))+1+len([]rune(word)) <= width {
				line += " " + word
			} else {
				printTUIRow(width, line, color)
				line = word
			}
		}
		if line != "" {
			printTUIRow(width, line, color)
		}
	}
}

func tuiConnect(in *bufio.Reader) error {
	profiles := discoverAWGProfiles()
	profilePath := ""
	var err error
	createdProfile := false
	if len(profiles) == 0 {
		drawTUIPage("FIRST CONNECTION", "No saved profile was found.", "Oberon will register one WARP key, save its profile, and reuse it on later connections.")
		profile, err := registerWarpWithSpinner("Registering your WARP account")
		if err != nil {
			return err
		}
		profilePath, err = writeNewWarpProfile(profile)
		if err != nil {
			return err
		}
		createdProfile = true
	} else {
		var ok bool
		profilePath, ok, err = chooseLocalProfile(in, profiles)
		if err != nil || !ok {
			return err
		}
	}
	cfg, err := parseAWGConfig(profilePath)
	if err != nil {
		return err
	}

	candidates := mustFastCandidates()
	scanLines := []string{filepath.Base(profilePath), fmt.Sprintf("Candidates: %d", len(candidates)), "Live scan progress appears below."}
	if createdProfile {
		scanLines = append(scanLines, "New profile saved; its WARP key will be reused next time.")
	}
	drawTUIPage("SCANNING ENDPOINTS", "Testing the saved profile before every connection.", scanLines...)
	hits, err := scanEndpointsWithProgress(cfg, candidates, 16, 900*time.Millisecond, newScanProgressReporter(len(candidates)))
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("no endpoint completed an AmneziaWG handshake")
	}
	cfg.peer["endpoint"] = hits[0].Endpoint
	drawTUIPage("ENDPOINT SELECTED", "Fastest endpoint with a successful handshake.", hits[0].Endpoint, fmt.Sprintf("Latency: %d ms", hits[0].Latency.Milliseconds()), "Starting Oberon with AmneziaWG WARP and both local proxies.")
	return runProxySession(cfg, defaultSOCKSAddress, defaultHTTPAddress)
}

func tuiCreateWarpKey(in *bufio.Reader) error {
	profiles := discoverAWGProfiles()
	profilePath := ""
	if len(profiles) > 0 {
		selected, ok, err := chooseLocalProfile(in, profiles)
		if err != nil || !ok {
			return err
		}
		profilePath = selected
	}

	registrationLines := []string{}
	if profilePath != "" {
		registrationLines = append(registrationLines, filepath.Base(profilePath))
	}
	drawTUIPage("REGISTERING WARP", "Creating a new key for the selected profile.", registrationLines...)
	profile, err := registerWarpWithSpinner("Registering a new WARP key")
	if err != nil {
		return err
	}

	if profilePath == "" {
		profilePath, err = writeNewWarpProfile(profile)
		if err != nil {
			return err
		}
		drawTUIPage("PROFILE CREATED", "The new WARP key and profile have been saved.", profilePath)
		waitPrompt(in)
		return nil
	}

	original, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	updated, err := applyWarpProfileToConfig(string(original), profile)
	if err != nil {
		return err
	}
	backupPath, err := backupProfile(profilePath, original)
	if err != nil {
		return fmt.Errorf("backup profile before applying the new key: %w", err)
	}
	if err := os.WriteFile(profilePath, []byte(updated), 0600); err != nil {
		return fmt.Errorf("save updated profile: %w (original backup: %s)", err, backupPath)
	}
	if err := os.Chmod(profilePath, 0600); err != nil {
		return fmt.Errorf("secure updated profile: %w (original backup: %s)", err, backupPath)
	}
	drawTUIPage("KEY APPLIED", "The saved profile now uses the new WARP key.", profilePath, "Previous profile backup: "+backupPath)
	waitPrompt(in)
	return nil
}

func tuiScan(in *bufio.Reader) error {
	profiles := discoverAWGProfiles()
	if len(profiles) == 0 {
		return fmt.Errorf("no valid .conf profile found beside oberon.exe or in the current folder")
	}
	profilePath, ok, err := chooseLocalProfile(in, profiles)
	if err != nil || !ok {
		return err
	}
	cfg, err := parseAWGConfig(profilePath)
	if err != nil {
		return err
	}

	candidates := mustFastCandidates()
	drawTUIPage("SCANNING ENDPOINTS", "Checking every candidate for a successful handshake.", filepath.Base(profilePath), fmt.Sprintf("Candidates: %d", len(candidates)), "Live scan progress appears below.")
	hits, err := scanEndpointsWithProgress(cfg, candidates, 16, 900*time.Millisecond, newScanProgressReporter(len(candidates)))
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("no working handshake found among %d scanned endpoints", len(candidates))
	}

	items := make([]pickerItem, 0, len(hits))
	for i, hit := range hits {
		category := "WORKING"
		if i == 0 {
			category = "BEST"
		}
		items = append(items, pickerItem{
			category: category,
			title:    hit.Endpoint,
			detail:   fmt.Sprintf("%d ms", hit.Latency.Milliseconds()),
		})
	}
	status := fmt.Sprintf("Working handshakes: %d   |   No handshake: %d", len(hits), len(candidates)-len(hits))
	selected, ok, err := pickTUIItem(in, "SCAN ENDPOINTS", "Sorted by measured latency. BEST is the fastest working endpoint.", status, items)
	if err != nil || !ok {
		return err
	}
	cfg.peer["endpoint"] = hits[selected].Endpoint
	drawTUIPage("ENDPOINT SELECTED", "Connecting with the endpoint you chose.", hits[selected].Endpoint, fmt.Sprintf("Latency: %d ms", hits[selected].Latency.Milliseconds()), "Starting Oberon with AmneziaWG WARP and both local proxies.")
	return runProxySession(cfg, defaultSOCKSAddress, defaultHTTPAddress)
}

func discoverAWGProfiles() []string {
	var directories []string
	if executable, err := os.Executable(); err == nil {
		directories = append(directories, filepath.Dir(executable))
	}
	if working, err := os.Getwd(); err == nil {
		directories = append(directories, working)
	}
	return discoverAWGProfilesIn(directories)
}


func discoverAWGProfilesIn(directories []string) []string {
	seen := make(map[string]bool)
	var profiles []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".conf") {
				continue
			}
			path, err := filepath.Abs(filepath.Join(directory, entry.Name()))
			if err != nil || seen[filepath.Clean(path)] {
				continue
			}
			if _, err := parseAWGConfig(path); err != nil {
				continue
			}
			seen[filepath.Clean(path)] = true
			profiles = append(profiles, path)
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		iPreferred := strings.EqualFold(filepath.Base(profiles[i]), "warp-awg.conf")
		jPreferred := strings.EqualFold(filepath.Base(profiles[j]), "warp-awg.conf")
		if iPreferred != jPreferred {
			return iPreferred
		}
		return strings.ToLower(profiles[i]) < strings.ToLower(profiles[j])
	})
	return profiles
}

func writeNewWarpProfile(profile warpProfile) (string, error) {
	var directories []string
	if executable, err := os.Executable(); err == nil {
		directories = append(directories, filepath.Dir(executable))
	}
	if working, err := os.Getwd(); err == nil {
		directories = append(directories, working)
	}
	if len(directories) == 0 {
		directories = append(directories, ".")
	}

	seen := make(map[string]bool)
	var failures []string
	contents := []byte(renderWarpConfig(profile, profile.Endpoint))
	for _, directory := range directories {
		cleanDirectory := filepath.Clean(directory)
		if seen[cleanDirectory] {
			continue
		}
		seen[cleanDirectory] = true
		for index := 0; ; index++ {
			name := "warp-awg.conf"
			if index > 0 {
				name = fmt.Sprintf("warp-awg-%d.conf", index)
			}
			path := filepath.Join(directory, name)
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if os.IsExist(err) {
				continue
			}
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", path, err))
				break
			}
			n, writeErr := file.Write(contents)
			closeErr := file.Close()
			if writeErr == nil && n != len(contents) {
				writeErr = io.ErrShortWrite
			}
			if writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				_ = os.Remove(path)
				failures = append(failures, fmt.Sprintf("%s: %v", path, writeErr))
				break
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("write new WARP profile failed (%s)", strings.Join(failures, "; "))
}

func registerWarpWithSpinner(label string) (warpProfile, error) {
	stop := startTerminalSpinner(label)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return registerWarp(ctx, newWarpHTTPClient(20*time.Second))
}

func chooseLocalProfile(in *bufio.Reader, profiles []string) (string, bool, error) {
	if len(profiles) == 1 {
		return profiles[0], true, nil
	}
	items := make([]pickerItem, 0, len(profiles))
	for _, path := range profiles {
		items = append(items, pickerItem{
			category: "PROFILE",
			title:    filepath.Base(path),
			detail:   shortenTUIPath(path, 18),
		})
	}
	status := fmt.Sprintf("Found %d valid profiles beside Oberon or in the current folder", len(profiles))
	index, ok, err := pickTUIItem(in, "CHOOSE A PROFILE", "Use the arrow keys and press Enter.", status, items)
	if err != nil || !ok {
		return "", ok, err
	}
	return profiles[index], true, nil
}

func pickTUIItem(in *bufio.Reader, title, subtitle, status string, items []pickerItem) (int, bool, error) {
	if len(items) == 0 {
		return -1, false, fmt.Errorf("there are no selectable items")
	}
	if isInteractiveTerminal() {
		selected, canceled := 0, false
		err := withRawTerminal(func() error {
			for {
				drawItemPicker(title, subtitle, status, items, selected)
				key, err := readPickerKey(in)
				if err != nil {
					return err
				}
				switch key {
				case "up", "k":
					if selected > 0 {
						selected--
					}
				case "down", "j":
					if selected < len(items)-1 {
						selected++
					}
				case "enter":
					return nil
				case "q":
					canceled = true
					return nil
				}
			}
		})
		if err == nil {
			return selected, !canceled, nil
		}
		fmt.Printf("\nArrow-key selection is unavailable: %v\nUsing numbered selection instead.\n", err)
	}

	printNumberedItems(title, subtitle, status, items)
	choice, err := readPrompt(in, "Select a number, or Q to cancel: ")
	if err != nil {
		return -1, false, err
	}
	if strings.EqualFold(choice, "q") {
		return -1, false, nil
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > len(items) {
		return -1, false, fmt.Errorf("choose a number from 1 to %d", len(items))
	}
	return index - 1, true, nil
}

func drawItemPicker(title, subtitle, status string, items []pickerItem, selected int) {
	const visibleItems = 10
	start := 0
	if selected >= visibleItems {
		start = selected - visibleItems + 1
	}
	end := start + visibleItems
	if end > len(items) {
		end = len(items)
	}
	drawTUIFrame(title, subtitle, func(width int) {
		printTUIWrapped(width, status, "1;37")
		printTUIRow(width, "", "")
		for index := start; index < end; index++ {
			marker := " "
			color := "2"
			if index == selected {
				marker = ">"
				color = "1;36"
			} else if items[index].category == "BEST" {
				color = "32;1"
			}
			row := fmt.Sprintf("%s %02d %-8s %-34s %s", marker, index+1, items[index].category, shortenTUIText(items[index].title, 34), shortenTUIText(items[index].detail, 12))
			printTUIRow(width, row, color)
		}
		if start > 0 || end < len(items) {
			printTUIRow(width, fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(items)), "2")
		}
		printTUIRow(width, "UP/DOWN or J/K  move  ·  ENTER  select  ·  Q  back", "36")
	})
}

func printNumberedItems(title, subtitle, status string, items []pickerItem) {
	drawTUIFrame(title, subtitle, func(width int) {
		printTUIWrapped(width, status, "1;37")
		for index, item := range items {
			row := fmt.Sprintf("[%02d] %-8s %-34s %s", index+1, item.category, shortenTUIText(item.title, 34), shortenTUIText(item.detail, 12))
			printTUIRow(width, row, "37")
		}
	})
}

func shortenTUIText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit < 4 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func shortenTUIPath(path string, limit int) string {
	runes := []rune(path)
	if len(runes) <= limit {
		return path
	}
	if limit < 4 {
		return string(runes[len(runes)-limit:])
	}
	return "..." + string(runes[len(runes)-limit+3:])
}

func readPickerKey(in *bufio.Reader) (string, error) {
	key, err := in.ReadByte()
	if err != nil {
		return "", err
	}
	switch key {
	case '\r', '\n':
		return "enter", nil
	case 'q', 'Q':
		return "q", nil
	case 'j', 'J':
		return "j", nil
	case 'k', 'K':
		return "k", nil
	case 0x1b:
		prefix, err := in.ReadByte()
		if err != nil {
			return "", err
		}
		if prefix != '[' && prefix != 'O' {
			return "", nil
		}
		direction, err := in.ReadByte()
		if err != nil {
			return "", err
		}
		if direction == 'A' {
			return "up", nil
		}
		if direction == 'B' {
			return "down", nil
		}
	}
	return "", nil
}

func mustFastCandidates() []string {
	candidates, _ := endpointCandidates("fast")
	return candidates
}

func replacePeerEndpoint(config, endpoint string) string {
	section, replaced := "", false
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(trimmed[1 : len(trimmed)-1])
			continue
		}
		if section == "peer" && strings.HasPrefix(strings.ToLower(trimmed), "endpoint") {
			lines[i] = "Endpoint = " + endpoint
			replaced = true
		}
	}
	if !replaced {
		return config
	}
	return strings.Join(lines, "\n")
}

func readPrompt(in *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := in.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func waitPrompt(in *bufio.Reader) {
	_, _ = readPrompt(in, "Press Enter to continue...")
}
