// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scopedconfig

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/dbtest"
	"gopkg.in/check.v1"
)

type S struct {
	storage *db.Storage
}

var _ = check.Suite(&S{})

func Test(t *testing.T) {
	check.TestingT(t)
}

func (s *S) SetUpSuite(c *check.C) {
	config.Set("database:url", "127.0.0.1:27017")
	config.Set("database:name", "provision_tests_s")
	var err error
	s.storage, err = db.Conn()
	c.Assert(err, check.IsNil)
}

func (s *S) TearDownSuite(c *check.C) {
	s.storage.Apps().Database.DropDatabase()
	s.storage.Close()
}

func (s *S) SetUpTest(c *check.C) {
	err := dbtest.ClearAllCollections(s.storage.Apps().Database)
	c.Assert(err, check.IsNil)
}

func (s *S) TestFindScopedConfig(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Scope, check.Equals, "x")
	conf.Add("a", "b")
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Envs, check.DeepEquals, []Entry{{Name: "a", Value: "b"}})
}

func (s *S) TestScopedConfigAdd(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "b")
	expected := []Entry{{Name: "a", Value: "b"}}
	c.Assert(conf.Envs, check.DeepEquals, expected)
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Envs, check.DeepEquals, expected)
}

func (s *S) TestScopedConfigAddPool(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.AddPool("p", "a", "b")
	expected := []PoolEntry{{Name: "p", Envs: []Entry{{Name: "a", Value: "b"}}}}
	c.Assert(conf.Envs, check.IsNil)
	c.Assert(conf.Pools, check.DeepEquals, expected)
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Envs, check.DeepEquals, []Entry{})
	c.Assert(conf.Pools, check.DeepEquals, expected)
}

func (s *S) TestScopedConfigRemove(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "b")
	conf.Add("c", "d")
	conf.Remove("a")
	c.Assert(conf.Envs, check.DeepEquals, []Entry{{Name: "c", Value: "d"}})
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Remove("c")
	c.Assert(conf.Envs, check.IsNil)
}

func (s *S) TestScopedConfigRemovePool(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "b")
	conf.AddPool("p", "a", "c")
	expected := []PoolEntry{{Name: "p", Envs: []Entry{{Name: "a", Value: "c"}}}}
	c.Assert(conf.Envs, check.DeepEquals, []Entry{{Name: "a", Value: "b"}})
	c.Assert(conf.Pools, check.DeepEquals, expected)
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.RemovePool("p", "a")
	c.Assert(conf.Envs, check.DeepEquals, []Entry{{Name: "a", Value: "b"}})
	c.Assert(conf.Pools, check.IsNil)
}

func (s *S) TestScopedConfigUpdateWith(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "overriden")
	conf.Add("c", "removed")
	conf.Add("d", "dval")
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	base := ScopedConfig{
		Scope: "ignored",
		Envs:  []Entry{{Name: "a", Value: "b"}, {Name: "b", Value: "c0"}, {Name: "c", Value: ""}},
		Pools: []PoolEntry{{Name: "p", Envs: []Entry{{Name: "b", Value: "c1"}}}},
		Extra: map[string]interface{}{"notcopied": "val"},
	}
	err = conf.UpdateWith(&base)
	c.Assert(err, check.IsNil)
	c.Assert(conf.Scope, check.Equals, "x")
	expectedEntries := []Entry{
		{Name: "a", Value: "b"},
		{Name: "b", Value: "c0"},
		{Name: "d", Value: "dval"},
	}
	expectedPoolEntries := []PoolEntry{{Name: "p", Envs: []Entry{{Name: "b", Value: "c1"}}}}
	sort.Sort(ConfigEntryList(expectedEntries))
	sort.Sort(ConfigPoolEntryList(expectedPoolEntries))
	sort.Sort(ConfigEntryList(conf.Envs))
	sort.Sort(ConfigPoolEntryList(conf.Pools))
	c.Assert(conf.Envs, check.DeepEquals, expectedEntries)
	c.Assert(conf.Pools, check.DeepEquals, expectedPoolEntries)
	c.Assert(conf.Extra, check.IsNil)
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	sort.Sort(ConfigEntryList(dbConf.Envs))
	sort.Sort(ConfigPoolEntryList(dbConf.Pools))
	c.Assert(dbConf.Envs, check.DeepEquals, expectedEntries)
	c.Assert(dbConf.Pools, check.DeepEquals, expectedPoolEntries)
	c.Assert(dbConf.Extra, check.IsNil)
}

func (s *S) TestScopedConfigSetGetExtra(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	err = conf.SetExtra("extra", "val")
	c.Assert(err, check.IsNil)
	val := conf.GetExtraString("extra")
	c.Assert(val, check.Equals, "val")
	invalidVal := conf.GetExtraString("extrasomething")
	c.Assert(invalidVal, check.Equals, "")
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	val = dbConf.GetExtraString("extra")
	c.Assert(val, check.Equals, "val")
	invalidVal = dbConf.GetExtraString("extrasomething")
	c.Assert(invalidVal, check.Equals, "")
}

func (s *S) TestScopedConfigSetExtraAtomic(c *check.C) {
	runtime.GOMAXPROCS(runtime.GOMAXPROCS(10))
	nRoutines := 50
	values := make([]bool, nRoutines)
	var wg sync.WaitGroup
	getTokenRoutine := func(wg *sync.WaitGroup, i int) {
		defer wg.Done()
		conf, err := FindScopedConfig("x")
		c.Assert(err, check.IsNil)
		isSet, err := conf.SetExtraAtomic("myvalue", fmt.Sprintf("val-%d", i))
		c.Assert(err, check.IsNil)
		values[i] = isSet
	}
	for i := 0; i < nRoutines; i++ {
		wg.Add(1)
		go getTokenRoutine(&wg, i)
	}
	wg.Wait()
	var valueSet *int
	for i := range values {
		if values[i] {
			c.Assert(valueSet, check.IsNil)
			valueSet = new(int)
			*valueSet = i
		}
	}
	c.Assert(valueSet, check.NotNil)
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	val := conf.GetExtraString("myvalue")
	c.Assert(val, check.Equals, fmt.Sprintf("val-%d", *valueSet))
}

func (s *S) TestScopedConfigPoolEntries(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "a0")
	conf.Add("b", "b0")
	conf.AddPool("p1", "a", "a1")
	conf.AddPool("p2", "b", "b1")
	entries := conf.PoolEntries("p1")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a1"},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
	entries = conf.PoolEntries("p2")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a0", Inherited: true},
		"b": Entry{Name: "b", Value: "b1"},
	})
	entries = conf.PoolEntries("p3")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a0", Inherited: true},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	entries = dbConf.PoolEntries("p1")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a1"},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
}

func (s *S) TestScopedConfigPoolEntriesWriteEmpty(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.add("", "a", "a0", true, false)
	conf.add("", "b", "b0", true, false)
	conf.add("p1", "a", "a1", true, false)
	conf.add("p2", "b", "b1", true, false)
	conf.add("p3", "a", "", true, false)
	entries := conf.PoolEntries("p1")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a1"},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
	entries = conf.PoolEntries("p2")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a0", Inherited: true},
		"b": Entry{Name: "b", Value: "b1"},
	})
	entries = conf.PoolEntries("p3")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: ""},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
	entries = conf.PoolEntries("p4")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a0", Inherited: true},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	entries = dbConf.PoolEntries("p1")
	c.Assert(entries, check.DeepEquals, EntryMap{
		"a": Entry{Name: "a", Value: "a1"},
		"b": Entry{Name: "b", Value: "b0", Inherited: true},
	})
}

func (s *S) TestScopedConfigPoolEntry(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "a0")
	conf.Add("b", "b0")
	conf.AddPool("p1", "a", "a1")
	conf.AddPool("p2", "b", "b1")
	entry := conf.PoolEntry("p1", "a")
	c.Assert(entry, check.Equals, "a1")
	entry = conf.PoolEntry("p1", "b")
	c.Assert(entry, check.Equals, "b0")
	entry = conf.PoolEntry("p2", "a")
	c.Assert(entry, check.Equals, "a0")
	entry = conf.PoolEntry("p2", "b")
	c.Assert(entry, check.Equals, "b1")
	entry = conf.PoolEntry("p3", "a")
	c.Assert(entry, check.Equals, "a0")
	entry = conf.PoolEntry("p3", "b")
	c.Assert(entry, check.Equals, "b0")
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	entry = dbConf.PoolEntry("p1", "a")
	c.Assert(entry, check.Equals, "a1")
}

func (s *S) TestScopedConfigResetEnvs(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Envs, check.IsNil)
	conf.Add("a", "a0")
	conf.ResetEnvs()
	c.Assert(conf.Envs, check.IsNil)
	conf.Add("b", "b0")
	conf.SaveEnvs()
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(dbConf.Envs, check.DeepEquals, []Entry{{Name: "b", Value: "b0"}})
}

func (s *S) TestScopedConfigResetPoolEnvs(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(conf.Envs, check.IsNil)
	conf.Add("a", "a0")
	conf.AddPool("p1", "a", "a0")
	conf.ResetPoolEnvs("p1")
	c.Assert(conf.Pools, check.HasLen, 0)
	conf.ResetPoolEnvs("")
	c.Assert(conf.Envs, check.IsNil)
	conf.Add("b", "b0")
	conf.AddPool("p1", "b", "b0")
	conf.SaveEnvs()
	dbConf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	c.Assert(dbConf.Envs, check.DeepEquals, []Entry{{Name: "b", Value: "b0"}})
	c.Assert(dbConf.Pools, check.DeepEquals, []PoolEntry{{Name: "p1", Envs: []Entry{{Name: "b", Value: "b0"}}}})
}

func (s *S) TestScopedConfigFilterPools(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	conf.Add("a", "a0")
	conf.Add("b", "b0")
	conf.AddPool("p1", "a", "a1")
	conf.AddPool("p2", "b", "b1")
	conf.FilterPools([]string{"p1"})
	sort.Sort(ConfigEntryList(conf.Envs))
	c.Assert(conf.Envs, check.DeepEquals, []Entry{{Name: "a", Value: "a0"}, {Name: "b", Value: "b0"}})
	c.Assert(conf.Pools, check.DeepEquals, []PoolEntry{{Name: "p1", Envs: []Entry{{Name: "a", Value: "a1"}}}})
}

func (s *S) TestScopedConfigAddEmpty(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	type myStruct struct {
		X string
	}
	conf.Add("a", myStruct{X: "999"})
	conf.Add("b", &myStruct{X: "123"})
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf.AddPool("p1", "a", myStruct{})
	conf.AddPool("p1", "b", &myStruct{})
	err = conf.SaveEnvs()
	c.Assert(err, check.IsNil)
	conf, err = FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	entryMap := conf.PoolEntries("p1")
	c.Assert(entryMap["a"], check.DeepEquals, Entry{Name: "a", Value: map[string]interface{}{"x": "999"}, Inherited: true})
	c.Assert(entryMap["b"], check.DeepEquals, Entry{Name: "b", Value: map[string]interface{}{"x": "123"}, Inherited: true})
}

func (s *S) TestScopedConfigMarshal(c *check.C) {
	tests := []struct {
		A interface{}
		B []Entry
	}{
		{map[string]interface{}{
			"v1": "a",
			"v2": 1,
			"v3": 1.1,
		}, []Entry{
			{Name: "v1", Value: "a"},
			{Name: "v2", Value: float64(1)},
			{Name: "v3", Value: 1.1},
		}},
		{struct {
			V1 string
			V2 int
			V3 float64
		}{
			V1: "a",
			V2: 1,
			V3: 1.1,
		}, []Entry{
			{Name: "V1", Value: "a"},
			{Name: "V2", Value: float64(1)},
			{Name: "V3", Value: 1.1},
		}},
		{struct {
			V1 string `json:"v1"`
			V2 int
			V3 float64
		}{
			V1: "a",
			V2: 1,
			V3: 1.1,
		}, []Entry{
			{Name: "V2", Value: float64(1)},
			{Name: "V3", Value: 1.1},
			{Name: "v1", Value: "a"},
		}},
		{struct {
			V1 string
			V2 int
			V3 float64
		}{
			V1: "a",
			V2: 0,
			V3: 1.1,
		}, []Entry{
			{Name: "V1", Value: "a"},
			{Name: "V2", Value: float64(0)},
			{Name: "V3", Value: 1.1},
		}},
	}
	for i, t := range tests {
		conf, err := FindScopedConfig(fmt.Sprintf("x%d", i))
		c.Assert(err, check.IsNil)
		err = conf.Marshal(t.A)
		c.Assert(err, check.IsNil)
		sort.Sort(ConfigEntryList(conf.Envs))
		c.Assert(conf.Envs, check.DeepEquals, t.B, check.Commentf("test %d failed", i+1))
	}
}

func (s *S) TestScopedConfigEntryMapUnmarshalWithMap(c *check.C) {
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	err = conf.Marshal(map[string]interface{}{
		"v1": "a",
		"v2": 1,
		"v3": 1.1,
	})
	c.Assert(err, check.IsNil)
	entries := conf.PoolEntries("")
	var result map[string]interface{}
	err = entries.Unmarshal(&result)
	c.Assert(err, check.IsNil)
	c.Assert(result, check.DeepEquals, map[string]interface{}{
		"v1":          "a",
		"v2":          float64(1),
		"v3":          1.1,
		"v1inherited": false,
		"v2inherited": false,
		"v3inherited": false,
	})
}

func (s *S) TestScopedConfigEntryMapUnmarshalWithStruct(c *check.C) {
	type baseStruct struct {
		V1 string
		V2 int
		V3 float64
	}
	expected := baseStruct{
		V1: "a",
		V2: 1,
		V3: 1.1,
	}
	conf, err := FindScopedConfig("x")
	c.Assert(err, check.IsNil)
	err = conf.Marshal(expected)
	c.Assert(err, check.IsNil)
	entries := conf.PoolEntries("")
	var result baseStruct
	err = entries.Unmarshal(&result)
	c.Assert(err, check.IsNil)
	c.Assert(result, check.DeepEquals, expected)
}
