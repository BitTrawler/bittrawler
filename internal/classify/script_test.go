package classify

import "testing"

func TestDominantScript(t *testing.T) {
	t.Parallel()

	tests := map[string]script{
		"寄生兽 完全版 第1卷":     scriptCJK,
		"寒战2":             scriptCJK,
		"Пепел 2013":      scriptCyrillic,
		"한국 영화 2020":      scriptHangul,
		"The Batman 2020": scriptLatin,
		"Batman 蝙蝠侠":      scriptLatin,
		"AB 蝙蝠":           scriptNone,
		"1234":            scriptNone,
		"":                scriptNone,
	}

	for name, want := range tests {
		if got := dominantScript(name); got != want {
			t.Errorf("dominantScript(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestScriptFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config []string
		title  string
		drop   Drop
	}{
		{name: "default keeps everything", config: nil, title: "寄生兽 完全版"},
		{name: "cjk filtered", config: []string{"cjk"}, title: "寄生兽 完全版", drop: DropScript},
		{name: "cjk filter and extension", config: []string{"cjk"}, title: "寒战2.mp4", drop: DropScript},
		{name: "latin kept", config: []string{"cjk"}, title: "The Batman 2020"},
		{name: "mixed kept", config: []string{"cjk"}, title: "Batman 蝙蝠侠"},
		{name: "cyrillic not configured", config: []string{"cjk"}, title: "Пепел 2013"},
		{name: "cyrillic configured", config: []string{"cyrillic"}, title: "Пепел 2013", drop: DropScript},
		{name: "nonlatin drops cjk", config: []string{"nonlatin"}, title: "寄生兽 完全版", drop: DropScript},
		{name: "nonlatin keeps latin", config: []string{"nonlatin"}, title: "Some Movie 2020"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := New(Config{DropScripts: test.config}).Classify(test.title, nil)

			if result.Drop != test.drop {
				t.Fatalf("drop = %v, want %v", result.Drop, test.drop)
			}
		})
	}
}
