package diptest

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zond/diplicity/auth"
	"github.com/zond/diplicity/game"
	"github.com/zond/godip"
	"github.com/zond/godip/variants"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func permutations(arr godip.Nations) []godip.Nations {
	var helper func(godip.Nations, int)
	res := []godip.Nations{}

	helper = func(arr godip.Nations, n int) {
		if n == 1 {
			tmp := make(godip.Nations, len(arr))
			copy(tmp, arr)
			res = append(res, tmp)
		} else {
			for i := 0; i < n; i++ {
				helper(arr, n-1)
				if n%2 == 1 {
					tmp := arr[i]
					arr[i] = arr[n-1]
					arr[n-1] = tmp
				} else {
					tmp := arr[0]
					arr[0] = arr[n-1]
					arr[n-1] = tmp
				}
			}
		}
	}
	helper(arr, len(arr))
	return res
}

func TestInactiveMemberEjection(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("TestCreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"NoMerge":            true,
				"Desc":               gameDesc,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			AssertNil("Properties", "NewestPhaseMeta")
	})
	t.Run("TestReapDoesntEvict", func(t *testing.T) {
		env.GetRoute(game.TestReapInactiveWaitingPlayersRoute).Success()
		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			AssertNil("Properties", "NewestPhaseMeta")
	})
	t.Run("TestReapEvicts", func(t *testing.T) {
		user := env.GetRoute(game.IndexRoute).Success().GetValue("Properties", "User")
		(user.(map[string]interface{}))["ValidUntil"] = time.Now().Add(-24 * 30 * time.Hour)
		env.PutRoute(auth.TestUpdateUserRoute).Body(user).Success()
		env.GetRoute(game.TestReapInactiveWaitingPlayersRoute).QueryParams(url.Values{
			"max-staging-game-inactivity": []string{"0"},
		}).Success()
		WaitForEmptyQueue("game-ejectMember")
		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func TestPreferenceAllocation(t *testing.T) {
	members := game.AllocationMembers{
		{
			Prefs: godip.Nations{
				godip.England,
				godip.France,
				godip.Germany,
			},
		},
		{
			Prefs: godip.Nations{
				godip.France,
				godip.England,
				godip.Germany,
			},
		},
		{
			Prefs: godip.Nations{
				godip.Germany,
				godip.France,
				godip.England,
			},
		},
		{
			Prefs: godip.Nations{
				godip.Russia,
				godip.Turkey,
				godip.Italy,
				godip.Austria,
				godip.Germany,
				godip.France,
				godip.England,
			},
		},
		{
			Prefs: godip.Nations{
				godip.Russia,
				godip.Turkey,
				godip.Italy,
				godip.Austria,
				godip.Germany,
				godip.France,
				godip.England,
			},
		},
		{
			Prefs: godip.Nations{
				godip.Russia,
				godip.Turkey,
				godip.Italy,
				godip.Austria,
				godip.Germany,
				godip.France,
				godip.England,
			},
		},
		{
			Prefs: godip.Nations{
				godip.Russia,
				godip.Turkey,
				godip.Italy,
				godip.Austria,
				godip.Germany,
				godip.France,
				godip.England,
			},
		},
	}
	alloc, err := game.AllocateNations(members, variants.Variants["Classical"].Nations)
	if err != nil {
		t.Fatal(err)
	}
	if alloc[0] != godip.England {
		t.Errorf("Wanted England, got %v", alloc[0])
	}
	if alloc[1] != godip.France {
		t.Errorf("Wanted France, got %v", alloc[1])
	}
	if alloc[2] != godip.Germany {
		t.Errorf("Wanted Germany, got %v", alloc[2])
	}
	members[1].Prefs = godip.Nations{
		godip.England,
		godip.Germany,
		godip.France,
	}
	va := variants.Variants["Classical"]
	alloc, err = game.AllocateNations(members, va.Nations)
	if err != nil {
		t.Fatal(err)
	}
	if alloc[0] != godip.France {
		t.Errorf("Wanted France, got %v", alloc[0])
	}
	if alloc[1] != godip.England {
		t.Errorf("Wanted England, got %v", alloc[1])
	}
	if alloc[2] != godip.Germany {
		t.Errorf("Wanted Germany, got %v", alloc[2])
	}
	for i := range members {
		prefs := rand.Perm(len(va.Nations))
		members[i].Prefs = make(godip.Nations, len(va.Nations))
		for j, pref := range prefs {
			members[i].Prefs[j] = va.Nations[pref]
		}
	}
	alloc, err = game.AllocateNations(members, va.Nations)
	if err != nil {
		t.Fatal(err)
	}
	scorer := func(p godip.Nations) int {
		result := 0
		for memberIdx, nat := range p {
			member := members[memberIdx]
			for score, pref := range member.Prefs {
				if pref == nat {
					result += score
					break
				}
			}
		}
		return result
	}
	foundScore := scorer(alloc)
	bestScore := -1
	for _, perm := range permutations(va.Nations) {
		if thisScore := scorer(perm); bestScore == -1 || thisScore < bestScore {
			bestScore = thisScore
		}
	}
	if bestScore != foundScore {
		t.Errorf("Got %v, but best score was %v", foundScore, bestScore)
	}
}

func TestGameSorting(t *testing.T) {
	g := game.Games{
		{
			Desc:      "a",
			NMembers:  4,
			CreatedAt: time.Now().Add(-time.Minute),
		},
		{
			Desc:      "b",
			NMembers:  4,
			CreatedAt: time.Now().Add(-time.Hour),
		},
		{
			Desc:      "c",
			NMembers:  5,
			CreatedAt: time.Now(),
		},
	}
	sort.Sort(g)
	if g[0].Desc != "c" {
		t.Errorf("got %q, wanted 'c'", g[0].Desc)
	}
	if g[1].Desc != "b" {
		t.Errorf("got %q, wanted 'b'", g[1].Desc)
	}
	if g[2].Desc != "a" {
		t.Errorf("got %q, wanted 'a'", g[2].Desc)
	}
	g = game.Games{
		{
			Desc:      "a",
			NMembers:  3,
			CreatedAt: time.Now().Add(42 * time.Second),
		},
		{
			Desc:      "b",
			NMembers:  1,
			CreatedAt: time.Now().Add(33 * time.Second),
		},
	}
	sort.Sort(g)
	if g[0].Desc != "a" {
		t.Errorf("got %q, wanted 'a'", g[0].Desc)
	}
	if g[1].Desc != "b" {
		t.Errorf("got %q, wanted 'b'", g[1].Desc)
	}
}

var (
	uniqueMaxHated uint64 = uint64(time.Now().UnixNano() / 1000000000)
)

func TestGameMerging(t *testing.T) {
	maxHated := atomic.AddUint64(&uniqueMaxHated, 1)
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("CreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifySelfGameNoMerge", func(t *testing.T) {
		gameDesc2 := String("test-game")
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"MaxHated":           float64(maxHated),
				"Desc":               gameDesc2,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc2, "Properties", "Desc")
		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc2, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyNoMergeGameNoMerge", func(t *testing.T) {
		env2 := NewEnv().SetUID(String("fake"))
		gameDesc3 := String("test-game")
		env2.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc3,
				"MaxHated":           float64(maxHated),
				"NoMerge":            true,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc3, "Properties", "Desc")
		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc3, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyDifferentVariantGameNoMerge", func(t *testing.T) {
		env3 := NewEnv().SetUID(String("fake"))
		gameDesc4 := String("test-game")
		env3.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Fleet Rome",
				"MaxHated":           float64(maxHated),
				"Desc":               gameDesc4,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc4, "Properties", "Desc")
		env3.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc4, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyPrivateGameNoMerge", func(t *testing.T) {
		env6 := NewEnv().SetUID(String("fake"))
		gameDesc6 := String("test-game")
		env6.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc6,
				"MaxHated":           float64(maxHated),
				"Private":            true,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success()
		env6.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc6, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyEqualGameMerge", func(t *testing.T) {
		env4 := NewEnv().SetUID(String("fake"))
		gameDesc5 := String("test-game")
		env4.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc5,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Status(http.StatusTeapot)
		env4.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})

	t.Run("VerifyBannedGameNoMerge", func(t *testing.T) {
		env5 := NewEnv().SetUID(String("fake"))
		env5.GetRoute(game.IndexRoute).Success().
			Follow("bans", "Links").Success().
			Follow("create", "Links").Body(map[string]interface{}{
			"UserIds": []string{env5.GetUID(), env.GetUID()},
		}).Success()

		gameDesc6 := String("test-game")
		env5.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"Desc":               gameDesc6,
				"MaxHated":           float64(maxHated),
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc6, "Properties", "Desc")
		env5.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc6, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func TestCreateGameWithAlias(t *testing.T) {
	gameDesc := String("test-game")
	gameAlias := String("alias")
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Variant": "Classical",
			"NoMerge": true,
			"FirstMember": &game.Member{
				GameAlias: gameAlias,
			},
			"Desc":               gameDesc,
			"PhaseLengthMinutes": time.Duration(60),
		}).Success().
		AssertEq(gameDesc, "Properties", "Desc").
		Find(gameAlias, []string{"Properties", "Members"}, []string{"GameAlias"})
}

func TestCreateGameWithPrefs(t *testing.T) {
	envs := []*Env{
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
		NewEnv().SetUID(String("fake")),
	}

	gameDesc := String("test-game")
	envs[0].GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").
		Body(map[string]interface{}{
			"Variant": "Classical",
			"NoMerge": true,
			"FirstMember": &game.Member{
				NationPreferences: string(godip.Austria),
			},
			"SkipMuster":         true,
			"Desc":               gameDesc,
			"PhaseLengthMinutes": time.Duration(60),
		}).Success().
		AssertEq(gameDesc, "Properties", "Desc")

	for i := 1; i < len(envs); i++ {
		envs[i].GetRoute(game.IndexRoute).Success().
			Follow("open-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("join", "Links").Body(map[string]interface{}{
			"NationPreferences": string(variants.Variants["Classical"].Nations[i]),
		}).Success()
	}

	WaitForEmptyQueue("game-asyncStartGame")

	for i, env := range envs {
		env.GetRoute(game.IndexRoute).Success().
			Follow("my-started-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Find(variants.Variants["Classical"].Nations[i], []string{"Properties", "Members"}, []string{"Nation"})
	}
}

func TestCreateLeaveGame(t *testing.T) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	t.Run("TestCreateGame", func(t *testing.T) {
		env.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(map[string]interface{}{
				"Variant":            "Classical",
				"NoMerge":            true,
				"Desc":               gameDesc,
				"PhaseLengthMinutes": time.Duration(60),
			}).Success().
			AssertEq(gameDesc, "Properties", "Desc")

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			AssertNil("Properties", "NewestPhaseMeta")
	})

	t.Run("TestLeaveAndDestroyGame", func(t *testing.T) {
		env.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).
			Follow("leave", "Links").Success()

		env.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
}

func randString() *string {
	if rand.Int() > 0 {
		rval := fmt.Sprint(rand.Int())
		return &rval
	}
	return nil
}

func randRange() *string {
	rvalSlice := []string{}
	if rand.Int() > 0 {
		rvalSlice = append(rvalSlice, fmt.Sprint(rand.Int()))
	}
	if rand.Int() > 0 {
		rvalSlice = append(rvalSlice, fmt.Sprint(rand.Int()))
	}
	if len(rvalSlice) > 0 {
		rval := strings.Join(rvalSlice, ":")
		return &rval
	}
	return nil
}

func randInt() *string {
	if rand.Int() > 0 {
		rval := fmt.Sprint(rand.Int())
		return &rval
	}
	return nil
}

func randBool() *string {
	if rand.Int() > 0 {
		rval := "true"
		if rand.Int() > 0 {
			rval = "false"
		}
		return &rval
	}
	return nil
}

// Not really a test, but it forces the dev_appserver to create (or validate, if run with --require_indexes)
// indices for a lot of combinations of filters and lists.
func TestIndexCreation(t *testing.T) {
	routes := []string{
		game.ListMyStagingGamesRoute,
		game.ListMyStartedGamesRoute,
		game.ListMyFinishedGamesRoute,
		game.ListOpenGamesRoute,
		game.ListStartedGamesRoute,
		game.ListFinishedGamesRoute,
		game.ListMasteredStagingGamesRoute,
		game.ListMasteredStartedGamesRoute,
		game.ListMasteredFinishedGamesRoute,
	}
	filterParams := map[string]func() *string{
		"variant":                  randString,
		"min-reliability":          randRange,
		"min-quickness":            randRange,
		"max-hater":                randRange,
		"max-hated":                randRange,
		"min-rating":               randRange,
		"max-rating":               randRange,
		"only-private":             randRange,
		"nation-allocation":        randInt,
		"phase-length-minutes":     randRange,
		"conference-chat-disabled": randBool,
		"group-chat-disabled":      randBool,
		"private-chat-disabled":    randBool,
	}
	for i := 0; i < 100; i++ {
		for _, route := range routes {
			env := NewEnv().SetUID(String("fake"))
			req := env.GetRoute(route)
			queryParams := url.Values{}
			for param, gen := range filterParams {
				generated := gen()
				if generated != nil {
					queryParams[param] = []string{*generated}
				}
			}
			req.QueryParams(queryParams).Success()
		}
	}
}

func TestGameListFilters(t *testing.T) {
	t.Run("PrivateGames", func(t *testing.T) {
		testGameListFilters(t, true)
	})
	t.Run("PublicGames", func(t *testing.T) {
		testGameListFilters(t, false)
	})
}

func testGameListFilters(t *testing.T, private bool) {
	gameDesc := String("test-game")
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":            "Classical",
		"NoMerge":            true,
		"Desc":               gameDesc,
		"MaxHated":           10,
		"MaxHater":           10,
		"MinReliability":     10,
		"MinQuickness":       10,
		"MinRating":          10,
		"MaxRating":          100,
		"PhaseLengthMinutes": 60,
	}).Failure()
	env.PutRoute(game.DevUserStatsUpdateRoute).RouteParams("user_id", env.GetUID()).Body(map[string]interface{}{
		"UserId":      env.GetUID(),
		"Reliability": 10,
		"Quickness":   10,
		"Hated":       0,
		"Hater":       0,
		"TrueSkill": &game.TrueSkill{
			TrueSkillContent: game.TrueSkillContent{
				Rating: 20,
			},
		},
	}).Success()
	gameURLString := env.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":            "Classical",
		"NoMerge":            true,
		"Private":            private,
		"Desc":               gameDesc,
		"MaxHated":           10,
		"MaxHater":           10,
		"MinReliability":     10,
		"MinQuickness":       10,
		"MinRating":          10,
		"MaxRating":          100,
		"PhaseLengthMinutes": time.Duration(60),
	}).Success().
		AssertEq(gameDesc, "Properties", "Desc").
		Find("self", []string{"Links"}, []string{"Rel"}).GetValue("URL").(string)
	gameURL, err := url.Parse(gameURLString)
	if err != nil {
		panic(err)
	}
	gameURL.RawQuery = ""
	gameID := regexp.MustCompile(".*/Game/([^/]+).*").FindStringSubmatch(gameURL.String())[1]

	env2 := NewEnv().SetUID(String("fake"))

	env2.GetRoute(game.IndexRoute).Success().
		Follow("open-games", "Links").Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	gameResp := env2.GetURL(gameURL.String()).Success()
	gameResp.
		AssertLen(3, "Properties", "FailedRequirements")
	gameResp.Find("MinRating", []string{"Properties", "FailedRequirements"}, nil)
	gameResp.Find("MinQuickness", []string{"Properties", "FailedRequirements"}, nil)
	gameResp.Find("MinReliability", []string{"Properties", "FailedRequirements"}, nil)
	gameResp.AssertNotFind("join", []string{"Links"}, []string{"Rel"})

	env2.PostRoute("Member.Create").RouteParams("game_id", gameID).Body(map[string]string{}).Failure()

	env2.PutRoute(game.DevUserStatsUpdateRoute).RouteParams("user_id", env2.GetUID()).Body(map[string]interface{}{
		"UserId":      env.GetUID(),
		"Reliability": 10,
		"Quickness":   10,
		"Hated":       0,
		"Hater":       0,
		"TrueSkill": &game.TrueSkill{
			TrueSkillContent: game.TrueSkillContent{
				Rating: 20,
			},
		},
	}).Success()

	if !private {
		env2.GetRoute(game.IndexRoute).Success().
			Follow("open-games", "Links").Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	}

	env2.GetURL(gameURL.String()).Success().
		AssertNil("Properties", "FailedRequirements").
		Find("join", []string{"Links"}, []string{"Rel"})

	if !private {
		for _, f := range []filter{
			{
				"conference-chat-disabled",
				"false",
				true,
			},
			{
				"conference-chat-disabled",
				"true",
				false,
			},
			{
				"group-chat-disabled",
				"false",
				true,
			},
			{
				"group-chat-disabled",
				"true",
				false,
			},
			{
				"private-chat-disabled",
				"false",
				true,
			},
			{
				"private-chat-disabled",
				"true",
				false,
			},
			{
				"phase-length-minutes",
				"60:60",
				true,
			},
			{
				"phase-length-minutes",
				"60:",
				true,
			},
			{
				"phase-length-minutes",
				"61:",
				false,
			},
			{
				"phase-length-minutes",
				":60",
				true,
			},
			{
				"phase-length-minutes",
				":59",
				false,
			},
			{
				"phase-length-minutes",
				"61:1000",
				false,
			},
			{
				"phase-length-minutes",
				"0:59",
				false,
			},
			{
				"nation-allocation",
				"1",
				false,
			},
			{
				"only-private",
				"true",
				false,
			},
			{
				"variant",
				"Classical",
				true,
			},
			{
				"variant",
				"blapp",
				false,
			},
			{
				"min-reliability",
				"5:15",
				true,
			},
			{
				"min-reliability",
				"0:5",
				false,
			},
			{
				"min-reliability",
				"15:20",
				false,
			},
			{
				"min-quickness",
				"5:15",
				true,
			},
			{
				"min-quickness",
				"0:5",
				false,
			},
			{
				"min-quickness",
				"15:20",
				false,
			},
			{
				"min-rating",
				"5:15",
				true,
			},
			{
				"min-rating",
				"0:5",
				false,
			},
			{
				"min-rating",
				"15:20",
				false,
			},
			{
				"max-rating",
				"95:115",
				true,
			},
			{
				"max-rating",
				"10:15",
				false,
			},
			{
				"max-rating",
				"125:130",
				false,
			},
			{
				"max-hater",
				"5:15",
				true,
			},
			{
				"max-hater",
				"0:5",
				false,
			},
			{
				"max-hater",
				"15:25",
				false,
			},
			{
				"max-hated",
				"5:15",
				true,
			},
			{
				"max-hated",
				"0:5",
				false,
			},
			{
				"max-hated",
				"15:25",
				false,
			},
		} {
			res := env2.GetRoute(game.ListOpenGamesRoute).QueryParams(url.Values{
				f.name: []string{f.value},
			}).Success()
			if f.wantFind {
				res.Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			} else {
				res.AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
			}
		}
	}

	env2.GetURL(gameURL.String()).Success().Follow("join", "Links").Body(map[string]string{}).Success()
}

func verifyAnonymous() {
	for idx := range startedGameEnvs {
		for _, nat := range startedGameNats {
			natMember := startedGames[idx].Find(nat, []string{"Properties", "Members"}, []string{"Nation"})
			if nat == startedGameNats[idx] {
				natMember.Find(startedGameEnvs[idx].GetUID(), []string{"User", "Id"})
			} else {
				natMember.Find("Anonymous", []string{"User", "Name"})
				natMember.Find("", []string{"User", "Email"})
				natMember.Find("", []string{"User", "Id"})
			}
		}
	}
}

func verifyNotAnonymous() {
	for idx := range startedGameEnvs {
		for otherIdx, nat := range startedGameNats {
			natMember := startedGames[idx].Find(nat, []string{"Properties", "Members"}, []string{"Nation"})
			natMember.Find(startedGameEnvs[otherIdx].GetUID(), []string{"User", "Id"})
			natMember.Find("Fakey Fakeson", []string{"User", "Name"})
		}
	}
}

func TestAnonymousGames(t *testing.T) {
	t.Run("PrivateGames", func(t *testing.T) {
		t.Run("NotGunboat", func(t *testing.T) {
			t.Run("Anonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = true
					opts["Anonymous"] = true
				}, verifyAnonymous)
			})
			t.Run("NotAnonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = true
					opts["Anonymous"] = false
				}, verifyNotAnonymous)
			})
		})
		t.Run("Gunboat", func(t *testing.T) {
			t.Run("Anonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = true
					opts["Anonymous"] = true
					opts["DisablePrivateChat"] = true
					opts["DisableGroupChat"] = true
					opts["DisableConferenceChat"] = true
				}, verifyAnonymous)
			})
			t.Run("NotAnonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = true
					opts["Anonymous"] = false
					opts["DisablePrivateChat"] = true
					opts["DisableGroupChat"] = true
					opts["DisableConferenceChat"] = true
				}, verifyNotAnonymous)
			})
		})
	})
	t.Run("PublicGames", func(t *testing.T) {
		t.Run("NotGunboat", func(t *testing.T) {
			t.Run("Anonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = false
					opts["Anonymous"] = true
				}, verifyNotAnonymous)
			})
			t.Run("NotAnonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = false
					opts["Anonymous"] = false
				}, verifyNotAnonymous)
			})
		})
		t.Run("Gunboat", func(t *testing.T) {
			t.Run("Anonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = false
					opts["Anonymous"] = true
					opts["DisablePrivateChat"] = true
					opts["DisableGroupChat"] = true
					opts["DisableConferenceChat"] = true
				}, verifyAnonymous)
			})
			t.Run("NotAnonymous", func(t *testing.T) {
				withStartedGameOpts(func(opts map[string]interface{}) {
					opts["Private"] = false
					opts["Anonymous"] = false
					opts["DisablePrivateChat"] = true
					opts["DisableGroupChat"] = true
					opts["DisableConferenceChat"] = true
				}, verifyAnonymous)
			})
		})
	})
}

type filter struct {
	name     string
	value    string
	wantFind bool
}

func TestGameLists(t *testing.T) {
	env := NewEnv().SetUID(String("fake"))
	env.GetRoute(game.ListMyStagingGamesRoute).Success()
	env.GetRoute(game.ListMyStartedGamesRoute).Success()
	env.GetRoute(game.ListMyFinishedGamesRoute).Success()
	env.GetRoute(game.ListOpenGamesRoute).Success()
	env.GetRoute(game.ListStartedGamesRoute).Success()
	env.GetRoute(game.ListFinishedGamesRoute).Success()
}

func TestTrueSkillLinksFromFinishedGames(t *testing.T) {
	withStartedGame(func() {
		for idx, game := range startedGames {
			p := game.Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

			p.Follow("phase-states", "Links").Success().
				Find(startedGameNats[idx], []string{"Properties"}, []string{"Properties", "Nation"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      true,
			}).Success()
		}

		WaitForEmptyQueue("game-asyncResolvePhase")

		newGameDesc := String("test-game")

		opts := map[string]interface{}{
			"Variant":            "Classical",
			"NoMerge":            true,
			"Desc":               newGameDesc,
			"PhaseLengthMinutes": 60 * 24,
		}
		startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").
			Body(opts).Success().
			AssertEq(newGameDesc, "Properties", "Desc")
		newGameID := startedGameEnvs[0].GetRoute(game.IndexRoute).Success().
			Follow("my-staging-games", "Links").Success().
			Find(newGameDesc, []string{"Properties"}, []string{"Properties", "Desc"}).GetValue("Properties", "ID").(string)
		for _, env := range startedGameEnvs[1:] {
			env.GetRoute("Game.Load").RouteParams("id", newGameID).Success().
				Follow("join", "Links").Body(map[string]interface{}{}).Success()
		}

		WaitForEmptyQueue("game-asyncStartGame")

		for _, env := range startedGameEnvs {
			p := env.GetRoute("Game.Load").RouteParams("id", newGameID).Success().
				Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

			p.Follow("phase-states", "Links").Success().
				Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
			}).Success()
		}

		WaitForEmptyQueue("game-asyncResolvePhase")

		for _, env := range startedGameEnvs {
			p := env.GetRoute("Game.Load").RouteParams("id", newGameID).Success().
				Follow("phases", "Links").Success().
				Find("Movement", []string{"Properties"}, []string{"Properties", "Type"})

			p.Follow("phase-states", "Links").Success().
				Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
				Follow("update", "Links").Body(map[string]interface{}{
				"ReadyToResolve": true,
				"WantsDIAS":      true,
			}).Success()
		}

		WaitForEmptyQueue("game-asyncResolvePhase")
		WaitForEmptyQueue("game-reRateTrueSkills")
		WaitForEmptyQueue("game-updateUserStats")

		startedGameEnvs[0].GetRoute("Game.Load").RouteParams("id", newGameID).Success().
			Follow("game-result", "Links").Success().
			Follow("true-skills", "Links").Success()

	})
}

func TestWithoutMustering(t *testing.T) {
	gameDesc := String("test-game")
	env1 := NewEnv().SetUID(String("uid"))
	gameID := env1.GetRoute(game.IndexRoute).Success().
		Follow("create-game", "Links").Body(map[string]interface{}{
		"Variant":            "France vs Austria",
		"NoMerge":            true,
		"Desc":               gameDesc,
		"Private":            false,
		"SkipMuster":         true,
		"PhaseLengthMinutes": 60,
	}).Success().GetValue("Properties", "ID").(string)

	env1.GetRoute(game.ListMyStagingGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env1.GetRoute(game.ListMyStartedGamesRoute).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	env2 := NewEnv().SetUID(String("uid"))

	env2.GetRoute(game.ListOpenGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env2.GetRoute(game.ListStartedGamesRoute).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env2.GetRoute(game.ListMyStartedGamesRoute).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
		Follow("join", "Links").Body(map[string]interface{}{}).Success()

	WaitForEmptyQueue("game-asyncStartGame")
	WaitForEmptyQueue("game-asyncSendMsg")

	g := env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
	g.AssertEq(true, "Properties", "Mustered")
	g.AssertLen(1, "Properties", "NewestPhaseMeta").
		Find(1, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"})
	messages := g.Follow("channels", "Links").Success().
		AssertLen(1, "Properties").
		Find(gameID, []string{"Properties"}, []string{"Properties", "GameID"}).
		Follow("messages", "Links").Success()
	messages.AssertLen(2, "Properties")
	messages.Find(fmt.Sprintf("Welcome to the France vs Austria game %s. Have fun!", gameDesc), []string{"Properties"}, []string{"Properties", "Body"})
	messages.Find("Please note that all messages become public after the game ends.", []string{"Properties"}, []string{"Properties", "Body"})
	phases := g.Follow("phases", "Links").Success()
	phases.AssertLen(1, "Properties")
	phases.Find(godip.Movement, []string{"Properties"}, []string{"Properties", "Type"}).
		AssertEq(1.0, "Properties", "PhaseOrdinal")

	env1.GetRoute(game.ListMyStartedGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env1.GetRoute(game.ListMyStagingGamesRoute).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

	env2.GetRoute(game.ListOpenGamesRoute).Success().
		AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env2.GetRoute(game.ListStartedGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	env2.GetRoute(game.ListMyStartedGamesRoute).Success().
		Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

}

func TestMustering(t *testing.T) {
	t.Run("NobodyReady", func(t *testing.T) {
		gameDesc := String("test-game")
		env1 := NewEnv().SetUID(String("uid"))
		gameID := env1.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").Body(map[string]interface{}{
			"Variant":            "France vs Austria",
			"NoMerge":            true,
			"Desc":               gameDesc,
			"Private":            false,
			"PhaseLengthMinutes": 60,
		}).Success().GetValue("Properties", "ID").(string)

		env1.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env2 := NewEnv().SetUID(String("uid"))

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(map[string]interface{}{}).Success()

		WaitForEmptyQueue("game-asyncStartGame")
		WaitForEmptyQueue("game-asyncSendMsg")

		g := env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g.AssertLen(1, "Properties", "NewestPhaseMeta").
			Find(1, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"})
		messages := g.Follow("channels", "Links").Success().
			AssertLen(1, "Properties").
			Find(gameID, []string{"Properties"}, []string{"Properties", "GameID"}).
			Follow("messages", "Links").Success()
		messages.AssertLen(2, "Properties")
		messages.Find(regexp.MustCompile("^Welcome to the France vs Austria game"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find("Please note that all messages become public after the game ends.", []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find(game.DiplicitySender, []string{"Properties"}, []string{"Properties", "Sender"})

		WaitForEmptyQueue("game-asyncSendMsg")
		WaitForEmptyQueue("game-sendMsgNotificationsToUsers")
		WaitForEmptyQueue("game-sendMsgNotificationsToFCM")
		WaitForEmptyQueue("game-sendMsgNotificationsToMail")

		env1.GetRoute(game.DevResolvePhaseTimeoutRoute).
			RouteParams("game_id", gameID, "phase_ordinal", "1").Success()

		env1.GetRoute(game.ListOpenGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
	})
	t.Run("NotReady", func(t *testing.T) {
		gameDesc := String("test-game")
		env1 := NewEnv().SetUID(String("uid"))
		gameID := env1.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").Body(map[string]interface{}{
			"Variant":            "France vs Austria",
			"NoMerge":            true,
			"Desc":               gameDesc,
			"Private":            false,
			"PhaseLengthMinutes": 60,
		}).Success().GetValue("Properties", "ID").(string)

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2 := NewEnv().SetUID(String("uid"))

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		msg1 := String("msg")

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("channels", "Links").Success().
			AssertNotFind("message", []string{"Links"}, []string{"Rel"})
		env1.GetRoute("Message.Create").RouteParams("game_id", gameID).Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": []string{"Austria", "France"},
		}).Failure()

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(map[string]interface{}{}).Success()

		WaitForEmptyQueue("game-asyncStartGame")
		WaitForEmptyQueue("game-asyncSendMsg")

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("channels", "Links").Success().
			AssertNotFind("message", []string{"Links"}, []string{"Rel"})
		env1.GetRoute("Message.Create").RouteParams("game_id", gameID).Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": []string{"Austria", "France"},
		}).Failure()

		g := env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g.AssertLen(1, "Properties", "NewestPhaseMeta").
			Find(1, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"})
		messages := g.Follow("channels", "Links").Success().
			AssertLen(1, "Properties").
			Find(gameID, []string{"Properties"}, []string{"Properties", "GameID"}).
			Follow("messages", "Links").Success()
		messages.AssertLen(2, "Properties")
		messages.Find(regexp.MustCompile("^Welcome to the France vs Austria game"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find("Please note that all messages become public after the game ends.", []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find(game.DiplicitySender, []string{"Properties"}, []string{"Properties", "Sender"})

		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		g = env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g.AssertEq(false, "Properties", "Mustered")
		g.Follow("phases", "Links").Success().
			Find("Spring", []string{"Properties"}, []string{"Properties", "Season"}).
			Follow("phase-states", "Links").Success().
			Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
			Follow("update", "Links").Body(map[string]interface{}{
			"ReadyToResolve": true,
		}).Success()

		env1.GetRoute(game.DevResolvePhaseTimeoutRoute).
			RouteParams("game_id", gameID, "phase_ordinal", "1").Success()

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(false, "Properties", "Mustered")

		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		WaitForEmptyQueue("game-asyncSendMsg")
		WaitForEmptyQueue("game-asyncSendMsg")

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("channels", "Links").Success().
			AssertNotFind("message", []string{"Links"}, []string{"Rel"})
		env1.GetRoute("Message.Create").RouteParams("game_id", gameID).Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": []string{"Austria", "France"},
		}).Failure()

		g = env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g.AssertNil("Properties", "NewestPhaseMeta")
		messages = g.Follow("channels", "Links").Success().
			AssertLen(1, "Properties").
			Find(gameID, []string{"Properties"}, []string{"Properties", "GameID"}).
			Follow("messages", "Links").Success()
		messages.AssertLen(3, "Properties")
		messages.Find(regexp.MustCompile("^Welcome to the France vs Austria game"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find(regexp.MustCompile("^Unfortunately \\d+ players weren't ready"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find("Please note that all messages become public after the game ends.", []string{"Properties"}, []string{"Properties", "Body"})
	})
	t.Run("Ready", func(t *testing.T) {
		gameDesc := String("test-game")
		env1 := NewEnv().SetUID(String("uid"))
		gameID := env1.GetRoute(game.IndexRoute).Success().
			Follow("create-game", "Links").Body(map[string]interface{}{
			"Variant":            "France vs Austria",
			"NoMerge":            true,
			"Desc":               gameDesc,
			"Private":            false,
			"PhaseLengthMinutes": 60,
		}).Success().GetValue("Properties", "ID").(string)

		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2 := NewEnv().SetUID(String("uid"))

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("join", "Links").Body(map[string]interface{}{}).Success()

		WaitForEmptyQueue("game-asyncStartGame")
		WaitForEmptyQueue("game-asyncSendMsg")

		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		g1 := env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g1.AssertEq(false, "Properties", "Mustered")
		g1.Follow("phases", "Links").Success().
			Find("Spring", []string{"Properties"}, []string{"Properties", "Season"}).
			Follow("phase-states", "Links").Success().
			Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
			Follow("update", "Links").Body(map[string]interface{}{
			"ReadyToResolve": true,
		}).Success()

		g2 := env2.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g2.AssertEq(false, "Properties", "Mustered")
		g2.Follow("phases", "Links").Success().
			Find("Spring", []string{"Properties"}, []string{"Properties", "Season"}).
			Follow("phase-states", "Links").Success().
			Find(false, []string{"Properties"}, []string{"Properties", "ReadyToResolve"}).
			Follow("update", "Links").Body(map[string]interface{}{
			"ReadyToResolve": true,
		}).Success()

		WaitForEmptyQueue("game-asyncResolvePhase")
		WaitForEmptyQueue("game-asyncSendMsg")

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(true, "Properties", "Mustered")

		env2.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			AssertEq(true, "Properties", "Mustered")

		env1.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env1.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		env2.GetRoute(game.ListOpenGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStagingGamesRoute).Success().
			AssertNotFind(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})
		env2.GetRoute(game.ListMyStartedGamesRoute).Success().
			Find(gameDesc, []string{"Properties"}, []string{"Properties", "Desc"})

		msg1 := String("msg")

		g := env1.GetRoute("Game.Load").RouteParams("id", gameID).Success()
		g.Find(1, []string{"Properties", "NewestPhaseMeta"}, []string{"PhaseOrdinal"})
		messages := g.Follow("channels", "Links").Success().
			AssertLen(1, "Properties").
			Find(gameID, []string{"Properties"}, []string{"Properties", "GameID"}).
			Follow("messages", "Links").Success()
		messages.AssertLen(3, "Properties")
		messages.Find(regexp.MustCompile("^Welcome to the France vs Austria game"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find(regexp.MustCompile("^All players are ready"), []string{"Properties"}, []string{"Properties", "Body"})
		messages.Find("Please note that all messages become public after the game ends.", []string{"Properties"}, []string{"Properties", "Body"})

		env1.GetRoute("Game.Load").RouteParams("id", gameID).Success().
			Follow("channels", "Links").Success().
			Follow("message", "Links").Body(map[string]interface{}{
			"Body":           msg1,
			"ChannelMembers": []string{"Austria", "France"},
		}).Success()
	})
}
