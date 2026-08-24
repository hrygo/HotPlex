package builtin

var generatedManifests = []PackageManifest{
	{
		Name:    "hotplex-cli",
		Version: "v1-0f16cb423ca6ce56845175809fddd8bdd6bdb6ac8b3911b19cd36cda38843089",
		Profile: ProfileRuntime,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1081, SHA256: "8509e03bac5d2cefb07f1ce30d6c2be12350868ce9e03a1a90f30c38f9a55c1c"},
			{Path: "references/cli-surface.generated.md", Size: 6963, SHA256: "8c4b0860bd73bfda2764f93d6ca39207c9fefdbe0063dda41ce59a96742bff2d"},
			{Path: "references/cron.md", Size: 2735, SHA256: "5e65b53ca00b62d98f8270efb75673fc5e68ae148f9f3cd67d942ccea11676ff"},
			{Path: "references/diagnostics.md", Size: 749, SHA256: "e0b618f4730213f9f2f13f0bcd05c02ae5106c360be6fe41294e8d30b3c1c643"},
			{Path: "references/slack.md", Size: 906, SHA256: "7385dc3d631130d00e2a4d1b108b97567e0f3b727a3f537df0d0d9075a693d28"},
			{Path: "references/user-guide.md", Size: 1715, SHA256: "419913a8d4d211b6a85beba813e61a1a58be00f89927e31d00e9a0b7e14c0a6c"},
		},
	},
	{
		Name:    "hotplex-operator",
		Version: "v1-5d16c674e5b2028d7568501262d05b010deb6ee72ace5767da23f284cbea1567",
		Profile: ProfileOperator,
		Assets: []AssetManifest{
			{Path: "SKILL.md", Size: 1201, SHA256: "314de7ced1eadaff47357f9a11c40471f6d172796b135c090653566c2e82f44b"},
			{Path: "references/admin-audit.md", Size: 625, SHA256: "ba715c97d570527c907081b8bfa848568bacfabbd7bf21ffd5d32dad27b587b5"},
			{Path: "references/configuration.md", Size: 1310, SHA256: "676bf789ba2db5047f6ea866f38e610a5dc3969301698cf7b3c796d6002fb92f"},
			{Path: "references/initialization.md", Size: 4533, SHA256: "00dc672a70fc48aac522d305ecb4604c15da4143e2794ceb0b622e00af75f920"},
			{Path: "references/install-update.md", Size: 1419, SHA256: "bf025ace635262f68cdadc3139365f661593b767e54fcb06d7ae565d99a6d7b8"},
			{Path: "references/service-lifecycle.md", Size: 732, SHA256: "d85b61301259f974a5ce92c40cb17de6db3378ebbebc56880122b7c75a704215"},
		},
	},
}
