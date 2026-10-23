package server

import "regexp"

var storageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
