package dashassets

import "embed"

//go:embed css/* js/* fonts/*
var Assets embed.FS
