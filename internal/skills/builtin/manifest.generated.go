package builtin

var generatedManifests = []PackageManifest{
	{
		Name:    "hotplex-cli",
		Version: "v1-8a86bcdd60dcdc507a24c1bb8d78e1c7e686b26100b3e316c61551c61b5f70d4",
		Profile: ProfileRuntime,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1039, SHA256: "dbdf74e7280932f717efe48b1ca849fe034eb479727936119af40168b8ab4e86"},
			{Path: "references/cli-surface.generated.md", Size: 7070, SHA256: "476ca95bb1edea6e146bc93e578ca9bd3750d34f27214e78decb32a96cba9312"},
			{Path: "references/cron.md", Size: 2844, SHA256: "4fe41efcd97f27838301adbcf2c5a86a58499e325828c657f078277d83624725"},
			{Path: "references/diagnostics.md", Size: 857, SHA256: "8e1137a1007778e3cc57d5551eb609095753a6e4aee2c028d28b7afd59c7e547"},
			{Path: "references/slack.md", Size: 1054, SHA256: "0f46f60774ff1c99c6a9fbe21d2510a6c4983e65355a2fb5eac0a8de6267ba61"},
		},
	},
	{
		Name:    "hotplex-operator",
		Version: "v1-c0b2c43d73d295a8bed536d5ba7b0a6562befcd1db9e414f82829d790078e19e",
		Profile: ProfileOperator,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1203, SHA256: "7407b3d89b5400fb27e2044e762730ff8e13b789ba389daf518fe13bd30a34e9"},
			{Path: "references/admin-audit.md", Size: 742, SHA256: "01fadb1da1ce8245ca5b185de25fc7301781a689c1b59aa001d35f2a4a9e0f3f"},
			{Path: "references/configuration.md", Size: 1527, SHA256: "fb0452c9fbdc93b22ecb32973ad9fb2fea2f2a8ca27fbca6743ffb0cd0a9481c"},
			{Path: "references/install-update.md", Size: 1679, SHA256: "0a862b8966bf3a9f22e0a9170c8afac63159c43c78f7b2741230e724377b2782"},
			{Path: "references/service-lifecycle.md", Size: 845, SHA256: "eb542339a9abcd02254b936b12b480eb3962a28c0e34be2bd09bf174f7ad09e3"},
		},
	},
}
