package conf

import (
	"net/url"
	"regexp"
)

var (
	BuiltAt    string = "unknown"
	GitAuthor  string = "unknown"
	GitCommit  string = "unknown"
	Version    string = "dev"
	WebVersion string = "rolling"
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
	MaxBufferLimit int
)

var (
	RawIndexHTML string
	ManageHTML   string
	IndexHTML    string
)