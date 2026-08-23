package builtin

var generatedManifests = []PackageManifest{
	{
		Name:    "hotplex-cli",
		Version: "v1-f2c3912551d4a9cb07e43c0fcb1f409da26ab23d226890ffaa50af6e58c1b865",
		Profile: ProfileRuntime,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1039, SHA256: "dbdf74e7280932f717efe48b1ca849fe034eb479727936119af40168b8ab4e86"},
			{Path: "references/cli-surface.generated.md", Size: 7070, SHA256: "476ca95bb1edea6e146bc93e578ca9bd3750d34f27214e78decb32a96cba9312"},
			{Path: "references/cron.md", Size: 2583, SHA256: "ed800cf3cc2ddd6935b7316cfb41bf5f48aaa4a8af0b0180150540b8eca0ad24"},
			{Path: "references/diagnostics.md", Size: 857, SHA256: "8e1137a1007778e3cc57d5551eb609095753a6e4aee2c028d28b7afd59c7e547"},
			{Path: "references/slack.md", Size: 1054, SHA256: "0f46f60774ff1c99c6a9fbe21d2510a6c4983e65355a2fb5eac0a8de6267ba61"},
		},
	},
	{
		Name:    "hotplex-operator",
		Version: "v1-c87fc4d2971c9a92dda8bc42c4f3451df84e0c9c814569fdf6759f30da2ea459",
		Profile: ProfileOperator,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1242, SHA256: "c14e30c0cabc817ca4a01486ebb701fb058ddcc9409205f2b6f88a793067a17b"},
			{Path: "references/admin-audit.md", Size: 470, SHA256: "e1a42e16f58a52f0d7878301bf97aa362a938617bfd1d25e162ac1527ee744e1"},
			{Path: "references/configuration.md", Size: 595, SHA256: "529de67d1bcbcd1af8c4e3f2c45788b926f4c91c62e377161909c1674f4cbdb8"},
			{Path: "references/install-update.md", Size: 543, SHA256: "9486e397f23b631b69d3017c4ba5bc49fa04af78d349e0e1940c71ad2f212dac"},
			{Path: "references/service-lifecycle.md", Size: 534, SHA256: "2b90c975a13df8ee580b91c375610a5b379e32c427c636534bd8fd216958a294"},
		},
	},
}
