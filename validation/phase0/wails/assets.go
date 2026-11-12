package main

import (
	"io/fs"
)

func fsSub() (fs.FS, error) { return fs.Sub(assets, "assets") }
