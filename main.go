package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

// https://manpages.debian.org/testing/dpkg-dev/deb-changelog.5.en.html
type Entry struct {
	Package        string    `json:"package"`
	Version        string    `json:"version"`
	Distributions  string    `json:"distributions"`
	Metadata       string    `json:"metadata"`
	MaintainerName string    `json:"maintainer_name"`
	EmailAddress   string    `json:"email_address"`
	Date           time.Time `json:"date"`
	Changes        []Change  `json:"changes"`
}

type Change struct {
	Summary string   `json:"summary"`
	Details []Detail `json:"details"`
}

type Detail struct {
	Lines []string `json:"lines"`
}

const (
	changePrefix         = "  * "
	detailHeadPrefix     = "    - "
	detailTailPrefix     = "      "
	maintainerLinePrefix = " -- "
)

type parseState int

const (
	parseStateInitial parseState = iota
	parseStateInEntry
	parseStateInChange
	parseStateInDetail
)

func main() {
	flag.Usage = func() {
		basename := filepath.Base(os.Args[0])
		output := flag.CommandLine.Output()
		fmt.Fprintf(output, "%s - filter for Ubuntu Linux kernel changelog\n\n", basename)
		fmt.Fprintf(output, "Usage of %s:\n", basename)
		flag.PrintDefaults()
	}

	filename := flag.String("file", "-", `changelog filename ("-" for stdin)`)
	filter := flag.String("filter", ".", "regular expression to be matched for change summary and details.\nSee https://pkg.go.dev/regexp/syntax for syntax.")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(Version())
		return
	}

	if err := run(*filename, *filter); err != nil {
		log.Fatal(err)
	}
}

func Version() string {
	// copied from https://blog.lufia.org/entry/2020/12/18/002238
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	return info.Main.Version
}

func run(filename, filter string) error {
	filterRE, err := regexp.Compile(filter)
	if err != nil {
		return err
	}

	var entries []Entry
	if filename == "-" {
		entries, err = parseChangelog(os.Stdin)
		if err != nil {
			return err
		}
	} else {
		entries, err = parseChangelogFile(filename)
		if err != nil {
			return err
		}
	}

	filtered, err := filterEntries(entries, filterRE)
	if err != nil {
		return err
	}

	for i, entry := range filtered {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s\n", entry.String())
	}
	return nil
}

func parseChangelogFile(filename string) ([]Entry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseChangelog(bufio.NewReader(file))
}

func parseChangelog(r io.Reader) ([]Entry, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	var entries []Entry
	var entry *Entry
	var change *Change
	var detail *Detail
	state := parseStateInitial

	processChangeLine := func(line string) {
		entry.Changes = append(entry.Changes, Change{
			Summary: line[len(changePrefix):],
		})
		change = &entry.Changes[len(entry.Changes)-1]
		state = parseStateInChange
	}

	processDetailHeadLine := func(line string) {
		change.Details = append(change.Details, Detail{
			Lines: []string{line[len(detailHeadPrefix):]},
		})
		detail = &change.Details[len(change.Details)-1]
		state = parseStateInDetail
	}

	processDetailTailLine := func(line string) {
		detail.Lines = append(detail.Lines, line[len(detailTailPrefix):])
	}

	processMaintainerLine := func(line string) error {
		if err := parseMaintainerLine(entry, line); err != nil {
			return err
		}
		entries = append(entries, *entry)
		state = parseStateInitial
		return nil
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\n")
		if len(line) == 0 {
			continue
		}

		switch state {
		case parseStateInitial:
			var err error
			entry, err = parseEntryLine(line)
			if err != nil {
				return nil, err
			}
			state = parseStateInEntry
		case parseStateInEntry:
			if strings.HasPrefix(line, changePrefix) {
				processChangeLine(line)
			} else if strings.HasPrefix(line, maintainerLinePrefix) {
				if err := processMaintainerLine(line); err != nil {
					return nil, err
				}
			}
		case parseStateInChange:
			if strings.HasPrefix(line, changePrefix) {
				processChangeLine(line)
			} else if strings.HasPrefix(line, detailHeadPrefix) {
				processDetailHeadLine(line)
			} else if strings.HasPrefix(line, maintainerLinePrefix) {
				if err := processMaintainerLine(line); err != nil {
					return nil, err
				}
			}
		case parseStateInDetail:
			if strings.HasPrefix(line, changePrefix) {
				processChangeLine(line)
			} else if strings.HasPrefix(line, detailHeadPrefix) {
				processDetailHeadLine(line)
			} else if strings.HasPrefix(line, detailTailPrefix) {
				processDetailTailLine(line)
			} else if strings.HasPrefix(line, maintainerLinePrefix) {
				if err := processMaintainerLine(line); err != nil {
					return nil, err
				}
			}
		}
	}
	return entries, nil
}

var entryLineRegex = regexp.MustCompile(`^([^ ]+) +\(([^)]+)\) +([^;]+); +(.*)`)

func parseEntryLine(line string) (*Entry, error) {
	m := entryLineRegex.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("invalid format entry line: %s", line)
	}
	return &Entry{
		Package:       m[1],
		Version:       m[2],
		Distributions: m[3],
		Metadata:      m[4],
	}, nil
}

var maintainerLineRegex = regexp.MustCompile(`^` + maintainerLinePrefix + `([^<]+) +<([^>]+)> +(.*)`)

const entryDateFormat = "Mon, 02 Jan 2006 15:04:05 -0700"

func parseMaintainerLine(e *Entry, line string) error {
	m := maintainerLineRegex.FindStringSubmatch(line)
	if m == nil {
		return fmt.Errorf("invalid format maintainer line: %s", line)
	}
	e.MaintainerName = m[1]
	e.EmailAddress = m[2]
	d, err := time.Parse(entryDateFormat, m[3])
	if err != nil {
		return fmt.Errorf("parse date: %s, %s", m[3], err)
	}
	e.Date = d
	return nil
}

func filterEntries(entries []Entry, filter *regexp.Regexp) ([]Entry, error) {
	var matchedEntries []Entry
	var matchedEntry *Entry
	var matchedChange *Change

	appendChange := func(entry Entry, change Change) {
		if matchedEntry == nil {
			matchedEntries = append(matchedEntries, Entry{
				Package:        entry.Package,
				Version:        entry.Version,
				Distributions:  entry.Distributions,
				Metadata:       entry.Metadata,
				MaintainerName: entry.MaintainerName,
				EmailAddress:   entry.EmailAddress,
				Date:           entry.Date,
			})
			matchedEntry = &matchedEntries[len(matchedEntries)-1]
		}
		matchedEntry.Changes = append(matchedEntry.Changes, Change{
			Summary: change.Summary,
		})
		matchedChange = &matchedEntry.Changes[len(matchedEntry.Changes)-1]
	}

	appendDetail := func(entry Entry, change Change, detail Detail) {
		if matchedChange == nil {
			appendChange(entry, change)
		}
		matchedChange.Details = append(matchedChange.Details, detail)
	}

	for _, entry := range entries {
		matchedEntry = nil
		for _, change := range entry.Changes {
			if filter.MatchString(change.Summary) {
				appendChange(entry, change)
			} else {
				matchedChange = nil
			}
			for _, detail := range change.Details {
				if detail.Matches(filter) {
					appendDetail(entry, change, detail)
				}
			}
		}
	}
	return matchedEntries, nil
}

func (d *Detail) Matches(re *regexp.Regexp) bool {
	for _, line := range d.Lines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func (e *Entry) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) %s; %s\n", e.Package, e.Version, e.Distributions, e.Metadata)
	for _, change := range e.Changes {
		fmt.Fprintf(&b, changePrefix+"%s\n", change.Summary)
		for _, detail := range change.Details {
			for i, line := range detail.Lines {
				prefix := detailHeadPrefix
				if i > 0 {
					prefix = detailTailPrefix
				}
				fmt.Fprintf(&b, prefix+"%s\n", line)
			}
		}
	}
	fmt.Fprintf(&b, maintainerLinePrefix+"%s <%s> %s", e.MaintainerName, e.EmailAddress, e.Date.Format(entryDateFormat))
	return b.String()
}
