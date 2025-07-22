package conf

import (
	"net/url"
	"regexp"
)

var (
	BuiltAt    string
	GitAuthor  string
	GitCommit  string
	Version    string = "dev"
	WebVersion string
)

var (
	Conf *Config
	URL  *url.URL
)

var (
	SlicesMap       = make(map[string][]string)
	FilenameCharMap = make(map[string]string)
	PrivacyReg      []*regexp.Regexp
)

var (
	// StoragesLoaded loaded success if empty
	StoragesLoaded = false
)

var (
	RawIndexHTML string
	ManageHTML   string
	IndexHTML    string
)
