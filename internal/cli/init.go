package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/joelrmartinez/connie/internal/config"
)

// stdin is a test seam for the init wizard.
var stdin io.Reader = os.Stdin

const initHeader = `# connie saved search, written by connie init.
# Edit freely. Flags on the command line override this file.
# location.remote: us (US-open remote), anywhere, or off.
# pay.floor: optional annual figure; a score input, never a cutoff.
`

// runInit asks the per-user questions once and writes connie.yaml.
func runInit(args []string) int {
	fl := flag.NewFlagSet("init", flag.ContinueOnError)
	fl.SetOutput(stderr)
	var (
		defaults = fl.Bool("defaults", false, "write the defaults without asking (scripts, CI)")
		out      = fl.String("out", config.DefaultPath, "file to write")
		force    = fl.Bool("force", false, "overwrite an existing file")
	)
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if !*force {
		if _, err := os.Stat(*out); err == nil {
			return fail(fmt.Errorf("%s exists; use --force to overwrite", *out))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}

	cfg := config.Defaults()
	if !*defaults {
		var err error
		if cfg, err = askConfig(bufio.NewReader(stdin), stdout, cfg); err != nil {
			return fail(err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}
	body, err := config.Marshal(cfg)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(*out, append([]byte(initHeader), body...), 0o644); err != nil {
		return fail(fmt.Errorf("write %s: %w", *out, err))
	}
	fmt.Fprintf(stderr, "wrote %s; run `connie scan` to use it\n", *out)
	return 0
}

// askConfig walks the questions, offering def's values. A blank answer
// keeps the default; a bad answer is asked again.
func askConfig(in *bufio.Reader, out io.Writer, def *config.Config) (*config.Config, error) {
	c := *def
	q := prompter{in: in, out: out}

	titles, err := q.list("Job titles you want (comma-separated)", def.Titles)
	if err != nil {
		return nil, err
	}
	c.Titles = titles
	c.Variants = config.VariantsFor(titles)

	for {
		r, err := q.text("Remote roles: us (US-open only), anywhere, or off", def.Location.Remote)
		if err != nil {
			return nil, err
		}
		r = strings.ToLower(r)
		if r == config.RemoteUS || r == config.RemoteAnywhere || r == config.RemoteOff {
			c.Location.Remote = r
			break
		}
		fmt.Fprintln(out, "  please answer us, anywhere, or off")
	}

	if c.Location.Cities, err = q.list("Cities you can reach an office in (comma-separated, - for none)", def.Location.Cities); err != nil {
		return nil, err
	}
	if len(c.Location.Cities) > 0 {
		if c.Location.Hybrid, err = q.yesNo("Do hybrid or onsite roles in those cities count?", def.Location.Hybrid); err != nil {
			return nil, err
		}
	} else {
		c.Location.Hybrid = false
	}

	for {
		floor := ""
		if def.Pay.Floor > 0 {
			floor = strconv.Itoa(def.Pay.Floor)
		}
		s, err := q.text("Pay floor, annual (blank for none; ranks higher above it, never drops a job)", floor)
		if err != nil {
			return nil, err
		}
		s = strings.NewReplacer(",", "", "$", "", "_", "").Replace(s)
		if strings.HasSuffix(strings.ToLower(s), "k") {
			s = strings.TrimSuffix(strings.TrimSuffix(s, "k"), "K") + "000"
		}
		if s == "" || s == "-" {
			c.Pay.Floor = 0
			break
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			c.Pay.Floor = n
			break
		}
		fmt.Fprintln(out, "  please enter a whole number like 150000 or 150k")
	}

	if c.Pay.CountDescription, err = q.yesNo("Count pay found in description text?", def.Pay.CountDescription); err != nil {
		return nil, err
	}

	for {
		names, err := q.list("ATSes to scan (comma-separated: "+strings.Join(config.AllATS, ", ")+")", def.ATS)
		if err != nil {
			return nil, err
		}
		if bad := unknownATS(names); bad != "" {
			fmt.Fprintf(out, "  unknown ats %q\n", bad)
			continue
		}
		c.ATS = names
		break
	}
	return &c, nil
}

func unknownATS(names []string) string {
	for _, n := range names {
		ok := false
		for _, a := range config.AllATS {
			if strings.EqualFold(n, a) {
				ok = true
			}
		}
		if !ok {
			return n
		}
	}
	return ""
}

// prompter reads one answer per line. EOF on an empty line takes the
// default, so piping a short answer file works.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p prompter) text(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", question)
	}
	line, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read answer: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func (p prompter) list(question string, def []string) ([]string, error) {
	s, err := p.text(question, strings.Join(def, ", "))
	if err != nil {
		return nil, err
	}
	if s == "-" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out, nil
}

func (p prompter) yesNo(question string, def bool) (bool, error) {
	d := "n"
	if def {
		d = "y"
	}
	for {
		s, err := p.text(question+" (y/n)", d)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.out, "  please answer y or n")
	}
}
