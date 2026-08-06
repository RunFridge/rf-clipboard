// Package web embeds the pages served by rf-clipd.
package web

import _ "embed"

//go:embed index.html
var Index []byte

//go:embed index.ko.html
var IndexKo []byte

//go:embed privacy.html
var Privacy []byte

//go:embed privacy.ko.html
var PrivacyKo []byte
