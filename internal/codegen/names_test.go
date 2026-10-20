package codegen

import "testing"

func TestParseName(t *testing.T) {
	tests := []struct {
		in                          string
		pascal, camel, snake, kebab string
		wantErr                     bool
	}{
		{in: "users", pascal: "Users", camel: "users", snake: "users", kebab: "users"},
		{in: "user-profile", pascal: "UserProfile", camel: "userProfile", snake: "user_profile", kebab: "user-profile"},
		{in: "user_profile", pascal: "UserProfile", camel: "userProfile", snake: "user_profile", kebab: "user-profile"},
		{in: "userProfile", pascal: "UserProfile", camel: "userProfile", snake: "user_profile", kebab: "user-profile"},
		{in: "UserProfile", pascal: "UserProfile", camel: "userProfile", snake: "user_profile", kebab: "user-profile"},
		{in: "HTTPServer", pascal: "HttpServer", camel: "httpServer", snake: "http_server", kebab: "http-server"},
		{in: "My App", pascal: "MyApp", camel: "myApp", snake: "my_app", kebab: "my-app"},
		{in: "v2api", pascal: "V2api", camel: "v2api", snake: "v2api", kebab: "v2api"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "---", wantErr: true},
		{in: "2fast", wantErr: true},
		{in: "a/b", wantErr: true},
		{in: "a.b", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			n, err := ParseName(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if n.Pascal != tc.pascal || n.Camel != tc.camel || n.Snake != tc.snake || n.Kebab != tc.kebab {
				t.Fatalf("got %+v", n)
			}
		})
	}
}

func TestTrimSuffixWord(t *testing.T) {
	n, _ := ParseName("auth-guard")
	if got := n.trimSuffixWord("guard").Pascal; got != "Auth" {
		t.Fatalf("got %s", got)
	}
	n, _ = ParseName("guard")
	if got := n.trimSuffixWord("guard").Pascal; got != "Guard" {
		t.Fatalf("single word must stay, got %s", got)
	}
}

func TestValidPackage(t *testing.T) {
	for _, bad := range []string{"", "func", "1abc", "a-b"} {
		if validPackage(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if validPackage("user_profile") != nil {
		t.Error("user_profile rejected")
	}
}

func TestParseKind(t *testing.T) {
	for in, want := range map[string]Kind{"module": KindModule, "mo": KindModule, "s": KindService, "dto": KindDTO, "pi": KindPipe} {
		got, err := ParseKind(in)
		if err != nil || got != want {
			t.Errorf("ParseKind(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseKind("nope"); err == nil {
		t.Error("expected error")
	}
}
