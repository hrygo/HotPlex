package builtin

var generatedManifests = []PackageManifest{
	{
		Name:    "hotplex-cli",
		Version: "1",
		Profile: ProfileRuntime,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1039, SHA256: "dbdf74e7280932f717efe48b1ca849fe034eb479727936119af40168b8ab4e86"},
			{Path: "references/cli-surface.generated.md", Size: 5624, SHA256: "82669e0d8bac6d2e62e014dda66f3156053cec81b4ab5c439f7439344d801a08"},
			{Path: "references/cron.md", Size: 1604, SHA256: "8f5b7fcb3da38660788b4e02819388acd8a05cd0122eed4b796f701e2d8148c2"},
			{Path: "references/diagnostics.md", Size: 857, SHA256: "8e1137a1007778e3cc57d5551eb609095753a6e4aee2c028d28b7afd59c7e547"},
			{Path: "references/slack.md", Size: 860, SHA256: "66131217f6a790d777d44ed7a60af08e0c1b5326a469fd82cd130ff2da056c1d"},
		},
	},
	{
		Name:    "hotplex-operator",
		Version: "1",
		Profile: ProfileOperator,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1240, SHA256: "92fe9a7c4c97f20cbe99de737ab4a0a2e896a5394b7b5f09984fc7f653d8a6e6"},
			{Path: "references/admin-audit.md", Size: 470, SHA256: "e1a42e16f58a52f0d7878301bf97aa362a938617bfd1d25e162ac1527ee744e1"},
			{Path: "references/configuration.md", Size: 595, SHA256: "529de67d1bcbcd1af8c4e3f2c45788b926f4c91c62e377161909c1674f4cbdb8"},
			{Path: "references/install-update.md", Size: 543, SHA256: "9486e397f23b631b69d3017c4ba5bc49fa04af78d349e0e1940c71ad2f212dac"},
			{Path: "references/service-lifecycle.md", Size: 534, SHA256: "2b90c975a13df8ee580b91c375610a5b379e32c427c636534bd8fd216958a294"},
		},
	},
}
