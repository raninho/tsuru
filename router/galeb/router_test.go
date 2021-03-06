// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package galeb

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/dbtest"
	galebClient "github.com/tsuru/tsuru/router/galeb/client"
	"github.com/tsuru/tsuru/router/routertest"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) {
	check.TestingT(t)
}

type fakeGalebServer struct {
	sync.Mutex
	targets      map[string]interface{}
	pools        map[string]interface{}
	virtualhosts map[string]interface{}
	rules        map[string]interface{}
	items        map[string]map[string]interface{}
	ruleVh       map[string][]string
	idCounter    int
	router       *mux.Router
}

func NewFakeGalebServer() (*fakeGalebServer, error) {
	server := &fakeGalebServer{
		targets:      make(map[string]interface{}),
		pools:        make(map[string]interface{}),
		virtualhosts: make(map[string]interface{}),
		rules:        make(map[string]interface{}),
		ruleVh:       make(map[string][]string),
	}
	server.items = map[string]map[string]interface{}{
		"target":      server.targets,
		"pool":        server.pools,
		"virtualhost": server.virtualhosts,
		"rule":        server.rules,
	}
	r := mux.NewRouter()
	r.HandleFunc("/api/target", server.createTarget).Methods("POST")
	r.HandleFunc("/api/pool", server.createPool).Methods("POST")
	r.HandleFunc("/api/rule", server.createRule).Methods("POST")
	r.HandleFunc("/api/virtualhost", server.createVirtualhost).Methods("POST")
	r.HandleFunc("/api/{item}/{id}", server.findItem).Methods("GET")
	r.HandleFunc("/api/{item}/{id}", server.destroyItem).Methods("DELETE")
	r.HandleFunc("/api/{item}/search/findByName", server.findItemByNameHandler).Methods("GET")
	r.HandleFunc("/api/rule/{id}/parents", server.addRuleVirtualhost).Methods("PATCH")
	r.HandleFunc("/api/rule/{id}/parents", server.findVirtualhostByRule).Methods("GET")
	r.HandleFunc("/api/rule/{id}/parents/{vhid}", server.destroyRuleVirtualhost).Methods("DELETE")
	r.HandleFunc("/api/target/search/findByParentName", server.findTargetsByParent).Methods("GET")
	server.router = r
	return server, nil
}

func (s *fakeGalebServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Lock()
	defer s.Unlock()
	s.router.ServeHTTP(w, r)
}

func (s *fakeGalebServer) findItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item := mux.Vars(r)["item"]
	obj, ok := s.items[item][id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(obj)
}

type searchRsp struct {
	Embedded map[string][]interface{} `json:"_embedded"`
}

func makeSearchRsp(itemName string, items ...interface{}) searchRsp {
	return searchRsp{Embedded: map[string][]interface{}{itemName: items}}
}

func (s *fakeGalebServer) findItemByNameHandler(w http.ResponseWriter, r *http.Request) {
	itemName := mux.Vars(r)["item"]
	wantedName := r.URL.Query().Get("name")
	ret := s.findItemByName(itemName, wantedName)
	json.NewEncoder(w).Encode(makeSearchRsp(itemName, ret...))
}

func (s *fakeGalebServer) findItemByName(itemName string, wantedName string) []interface{} {
	items := s.items[itemName]
	var ret []interface{}
	for i, item := range items {
		name := item.(interface {
			GetName() string
		}).GetName()
		if name == wantedName {
			ret = append(ret, items[i])
		}
	}
	return ret
}

func (s *fakeGalebServer) findTargetsByParent(w http.ResponseWriter, r *http.Request) {
	parentName := r.URL.Query().Get("name")
	var pool *galebClient.Pool
	var ret []interface{}
	for _, item := range s.pools {
		p := item.(*galebClient.Pool)
		if p.Name == parentName {
			pool = p
		}
	}
	for i, item := range s.targets {
		target := item.(*galebClient.Target)
		if target.BackendPool == pool.FullId() {
			ret = append(ret, s.targets[i])
		}
	}
	json.NewEncoder(w).Encode(makeSearchRsp("target", ret...))
}

func (s *fakeGalebServer) destroyItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item := mux.Vars(r)["item"]
	_, ok := s.items[item][id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	delete(s.items[item], id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *fakeGalebServer) createTarget(w http.ResponseWriter, r *http.Request) {
	var target galebClient.Target
	target.Status = "OK"
	json.NewDecoder(r.Body).Decode(&target)
	targetsWithName := s.findItemByName("target", target.Name)
	for _, item := range targetsWithName {
		otherTarget := item.(*galebClient.Target)
		if otherTarget.BackendPool == target.BackendPool {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	s.idCounter++
	target.ID = s.idCounter
	target.Links.Self.Href = fmt.Sprintf("http://%s%s/%d", r.Host, r.URL.String(), target.ID)
	s.targets[strconv.Itoa(target.ID)] = &target
	w.Header().Set("Location", target.Links.Self.Href)
	w.WriteHeader(http.StatusCreated)
}

func (s *fakeGalebServer) createPool(w http.ResponseWriter, r *http.Request) {
	var pool galebClient.Pool
	pool.Status = "OK"
	json.NewDecoder(r.Body).Decode(&pool)
	poolsWithName := s.findItemByName("pool", pool.Name)
	if len(poolsWithName) > 0 {
		w.WriteHeader(http.StatusConflict)
		return
	}
	s.idCounter++
	pool.ID = s.idCounter
	pool.Links.Self.Href = fmt.Sprintf("http://%s%s/%d", r.Host, r.URL.String(), pool.ID)
	s.pools[strconv.Itoa(pool.ID)] = &pool
	w.Header().Set("Location", pool.Links.Self.Href)
	w.WriteHeader(http.StatusCreated)
}

func (s *fakeGalebServer) createRule(w http.ResponseWriter, r *http.Request) {
	var rule galebClient.Rule
	rule.Status = "OK"
	json.NewDecoder(r.Body).Decode(&rule)
	s.idCounter++
	rule.ID = s.idCounter
	rule.Links.Self.Href = fmt.Sprintf("http://%s%s/%d", r.Host, r.URL.String(), rule.ID)
	s.rules[strconv.Itoa(rule.ID)] = &rule
	w.Header().Set("Location", rule.Links.Self.Href)
	w.WriteHeader(http.StatusCreated)
}

func (s *fakeGalebServer) addRuleVirtualhost(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	parts := strings.Split(string(data), "\n")
	if len(parts) == 0 || parts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	vhId := parts[0][strings.LastIndex(parts[0], "/")+1:]
	baseRule := s.rules[id].(*galebClient.Rule)
	baseVirtualHost := s.virtualhosts[vhId].(*galebClient.VirtualHost)
	if baseRule == nil || baseVirtualHost == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.ruleVh[id] = append(s.ruleVh[id], vhId)
	w.WriteHeader(http.StatusNoContent)
}

func (s *fakeGalebServer) destroyRuleVirtualhost(w http.ResponseWriter, r *http.Request) {
	ruleId := mux.Vars(r)["id"]
	vhId := mux.Vars(r)["vhid"]
	idx := -1
	for i, currentVh := range s.ruleVh[ruleId] {
		if currentVh == vhId {
			idx = i
			break
		}
	}
	if idx == -1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.ruleVh[ruleId] = append(s.ruleVh[ruleId][:idx], s.ruleVh[ruleId][idx+1:]...)
	if len(s.ruleVh[ruleId]) == 0 {
		delete(s.ruleVh, ruleId)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *fakeGalebServer) findVirtualhostByRule(w http.ResponseWriter, r *http.Request) {
	ruleId := mux.Vars(r)["id"]
	var ret []interface{}
	for _, vhId := range s.ruleVh[ruleId] {
		ret = append(ret, s.virtualhosts[vhId])
	}
	json.NewEncoder(w).Encode(makeSearchRsp("virtualhost", ret...))
}

func (s *fakeGalebServer) createVirtualhost(w http.ResponseWriter, r *http.Request) {
	var virtualhost galebClient.VirtualHost
	virtualhost.Status = "OK"
	json.NewDecoder(r.Body).Decode(&virtualhost)
	if len(s.findItemByName("virtualhost", virtualhost.Name)) > 0 {
		w.WriteHeader(http.StatusConflict)
		return
	}
	s.idCounter++
	virtualhost.ID = s.idCounter
	virtualhost.Links.Self.Href = fmt.Sprintf("http://%s%s/%d", r.Host, r.URL.String(), virtualhost.ID)
	s.virtualhosts[strconv.Itoa(virtualhost.ID)] = &virtualhost
	w.Header().Set("Location", virtualhost.Links.Self.Href)
	w.WriteHeader(http.StatusCreated)
}

func init() {
	suite := &routertest.RouterSuite{
		SetUpSuiteFunc: func(c *check.C) {
			config.Set("routers:galeb:username", "myusername")
			config.Set("routers:galeb:password", "mypassword")
			config.Set("routers:galeb:domain", "galeb.com")
			config.Set("routers:galeb:type", "galeb")
			config.Set("database:url", "127.0.0.1:27017")
			config.Set("database:name", "router_galeb_tests")
		},
	}
	var server *httptest.Server
	var fakeServer *fakeGalebServer
	suite.SetUpTestFunc = func(c *check.C) {
		var err error
		fakeServer, err = NewFakeGalebServer()
		c.Assert(err, check.IsNil)
		server = httptest.NewServer(fakeServer)
		config.Set("routers:galeb:api-url", server.URL+"/api")
		gRouter, err := createRouter("galeb", "routers:galeb")
		c.Assert(err, check.IsNil)
		suite.Router = gRouter
		conn, err := db.Conn()
		c.Assert(err, check.IsNil)
		defer conn.Close()
		dbtest.ClearAllCollections(conn.Collection("router_galeb_tests").Database)
	}
	suite.TearDownTestFunc = func(c *check.C) {
		server.Close()
		c.Check(fakeServer.targets, check.DeepEquals, map[string]interface{}{})
		c.Check(fakeServer.pools, check.DeepEquals, map[string]interface{}{})
		c.Check(fakeServer.virtualhosts, check.DeepEquals, map[string]interface{}{})
		c.Check(fakeServer.rules, check.DeepEquals, map[string]interface{}{})
		c.Check(fakeServer.ruleVh, check.DeepEquals, map[string][]string{})
	}
	check.Suite(suite)
}
