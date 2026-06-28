package config

// fileConfig is the on-disk .treepi.toml shape. Pointer / optional fields let
// the loader detect which keys a layer actually set, so a higher layer overrides
// only the keys it specifies (a non-pointer zero value would clobber lower
// layers). The resolved Config is a separate, flat struct.
type fileConfig struct {
	Placement *placementFile      `toml:"placement"`
	Branch    *branchFile         `toml:"branch"`
	Merge     *mergeFile          `toml:"merge"`
	Snapshot  *snapshotFile       `toml:"snapshot"`
	Hooks     map[string]hookFile `toml:"hooks"`
}

type placementFile struct {
	BaseDir *string `toml:"base_dir"`
}

type branchFile struct {
	Template *string `toml:"template"`
	Trunk    *string `toml:"trunk"`
}

type mergeFile struct {
	Verify []string `toml:"verify"`
}

type snapshotFile struct {
	IncludeIgnored []string `toml:"include_ignored"`
}

type hookFile struct {
	Command   []string  `toml:"command"`
	Timeout   *Duration `toml:"timeout"`
	OnFailure *string   `toml:"on_failure"`
}
