// Package authn provides AuthN server for AIStore.
/*
 * Copyright (c) 2018-2022, NVIDIA CORPORATION. All rights reserved.
 */
package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/api/apc"
	"github.com/NVIDIA/aistore/api/authn"
	"github.com/NVIDIA/aistore/cmd/authn/tok"
	"github.com/NVIDIA/aistore/cmn"
	jsoniter "github.com/json-iterator/go"
)

func checkRESTItems(w http.ResponseWriter, r *http.Request, itemsAfter int, items []string) ([]string, error) {
	items, err := cmn.MatchRESTItems(r.URL.Path, itemsAfter, true, items)
	if err != nil {
		cmn.WriteErr(w, r, err)
		return nil, err
	}
	return items, err
}

//-------------------------------------
// auth server
//-------------------------------------
type Server struct {
	mux   *http.ServeMux
	h     *http.Server
	users *UserManager
}

var (
	svcName = "AuthN"
	Conf    = &authn.Config{}
)

func NewServer(mgr *UserManager) *Server {
	srv := &Server{users: mgr}
	srv.mux = http.NewServeMux()

	return srv
}

// Run public server to manage users and generate tokens
func (a *Server) Run() (err error) {
	portstring := fmt.Sprintf(":%d", Conf.Net.HTTP.Port)
	glog.Infof("Launching public server at %s", portstring)

	a.registerPublicHandlers()
	a.h = &http.Server{Addr: portstring, Handler: a.mux}
	if Conf.Net.HTTP.UseHTTPS {
		if err = a.h.ListenAndServeTLS(Conf.Net.HTTP.Certificate, Conf.Net.HTTP.Key); err == nil {
			return nil
		}
		goto rerr
	}
	if err = a.h.ListenAndServe(); err == nil {
		return nil
	}
rerr:
	if err != http.ErrServerClosed {
		glog.Errorf("Terminated with err: %v", err)
		return err
	}
	return nil
}

func (a *Server) registerHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(path, handler)
	if !strings.HasSuffix(path, "/") {
		a.mux.HandleFunc(path+"/", handler)
	}
}

func (a *Server) registerPublicHandlers() {
	a.registerHandler(apc.URLPathUsers.S, a.userHandler)
	a.registerHandler(apc.URLPathTokens.S, a.tokenHandler)
	a.registerHandler(apc.URLPathClusters.S, a.clusterHandler)
	a.registerHandler(apc.URLPathRoles.S, a.roleHandler)
	a.registerHandler(apc.URLPathDae.S, configHandler)
}

func (a *Server) userHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		a.httpUserDel(w, r)
	case http.MethodPost:
		a.httpUserPost(w, r)
	case http.MethodPut:
		a.httpUserPut(w, r)
	case http.MethodGet:
		a.httpUserGet(w, r)
	default:
		cmn.WriteErr405(w, r, http.MethodDelete, http.MethodGet, http.MethodPost, http.MethodPut)
	}
}

func (a *Server) tokenHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		a.httpRevokeToken(w, r)
	default:
		cmn.WriteErr405(w, r, http.MethodDelete)
	}
}

func (a *Server) clusterHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.httpSrvGet(w, r)
	case http.MethodPost:
		a.httpSrvPost(w, r)
	case http.MethodPut:
		a.httpSrvPut(w, r)
	case http.MethodDelete:
		a.httpSrvDelete(w, r)
	default:
		cmn.WriteErr405(w, r, http.MethodDelete, http.MethodGet, http.MethodPost, http.MethodPut)
	}
}

// Deletes existing token, a.k.a log out
func (a *Server) httpRevokeToken(w http.ResponseWriter, r *http.Request) {
	if _, err := checkRESTItems(w, r, 0, apc.URLPathTokens.L); err != nil {
		return
	}
	msg := &authn.TokenMsg{}
	if err := cmn.ReadJSON(w, r, msg); err != nil {
		return
	}
	if msg.Token == "" {
		cmn.WriteErrMsg(w, r, "empty token")
		return
	}
	secret := Conf.Secret()
	_, err := tok.DecryptToken(msg.Token, secret)
	if err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
	a.users.revokeToken(msg.Token)
}

func (a *Server) httpUserDel(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathUsers.L)
	if err != nil {
		return
	}

	if err = checkAuthorization(w, r); err != nil {
		return
	}
	if err := a.users.delUser(apiItems[0]); err != nil {
		glog.Errorf("Failed to delete user: %v\n", err)
		cmn.WriteErrMsg(w, r, "Failed to delete user: "+err.Error())
	}
}

func (a *Server) httpUserPost(w http.ResponseWriter, r *http.Request) {
	if apiItems, err := checkRESTItems(w, r, 0, apc.URLPathUsers.L); err != nil {
		return
	} else if len(apiItems) == 0 {
		a.userAdd(w, r)
	} else {
		a.userLogin(w, r)
	}
}

// Updates user credentials
func (a *Server) httpUserPut(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathUsers.L)
	if err != nil {
		return
	}
	if err = checkAuthorization(w, r); err != nil {
		return
	}

	userID := apiItems[0]
	updateReq := &authn.User{}
	err = jsoniter.NewDecoder(r.Body).Decode(updateReq)
	if err != nil {
		cmn.WriteErrMsg(w, r, "Invalid request")
		return
	}
	if glog.V(4) {
		glog.Infof("PUT user %q", userID)
	}
	if err := a.users.updateUser(userID, updateReq); err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
}

// Adds a new user to user list
func (a *Server) userAdd(w http.ResponseWriter, r *http.Request) {
	if err := checkAuthorization(w, r); err != nil {
		return
	}
	info := &authn.User{}
	if err := cmn.ReadJSON(w, r, info); err != nil {
		return
	}
	if err := a.users.addUser(info); err != nil {
		cmn.WriteErrMsg(w, r, fmt.Sprintf("Failed to add user: %v", err), http.StatusInternalServerError)
		return
	}
	if glog.V(4) {
		glog.Infof("Add user %q", info.ID)
	}
}

// Returns list of users (without superusers)
func (a *Server) httpUserGet(w http.ResponseWriter, r *http.Request) {
	items, err := checkRESTItems(w, r, 0, apc.URLPathUsers.L)
	if err != nil {
		return
	}

	if len(items) > 1 {
		cmn.WriteErrMsg(w, r, "invalid request")
		return
	}

	var users map[string]*authn.User
	if len(items) == 0 {
		if users, err = a.users.userList(); err != nil {
			cmn.WriteErr(w, r, err)
			return
		}
		for _, uInfo := range users {
			uInfo.Password = ""
		}
		writeJSON(w, users, "list users")
		return
	}

	uInfo, err := a.users.lookupUser(items[0])
	if err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
	uInfo.Password = ""
	clusters, err := a.users.clusterList()
	if err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
	for _, clu := range uInfo.ClusterACLs {
		if cInfo, ok := clusters[clu.ID]; ok {
			clu.Alias = cInfo.Alias
		}
	}
	writeJSON(w, uInfo, "user info")
}

// Checks if the request header contains valid admin credentials.
// (admin is created at deployment time and cannot be modified via API)
func checkAuthorization(w http.ResponseWriter, r *http.Request) error {
	s := strings.SplitN(r.Header.Get(apc.HdrAuthorization), " ", 2)
	if len(s) != 2 {
		err := errors.New("not authorized: invalid header")
		cmn.WriteErrMsg(w, r, err.Error(), http.StatusUnauthorized)
		return err
	}
	secret := Conf.Secret()
	tk, err := tok.DecryptToken(s[1], secret)
	if err != nil {
		cmn.WriteErrMsg(w, r, err.Error(), http.StatusUnauthorized)
		return err
	}
	if tk.Expires.Before(time.Now()) {
		err := fmt.Errorf("not authorized: %s", tk)
		cmn.WriteErrMsg(w, r, err.Error(), http.StatusUnauthorized)
		return err
	}
	if !tk.IsAdmin {
		err := fmt.Errorf("not authorized: requires admin (%s)", tk)
		cmn.WriteErrMsg(w, r, err.Error(), http.StatusUnauthorized)
		return err
	}
	return nil
}

// Generate a token for a user if provided credentials are valid.
// If a token is already issued and it is not expired yet then the old
// token is returned
func (a *Server) userLogin(w http.ResponseWriter, r *http.Request) {
	var err error
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathUsers.L)
	if err != nil {
		return
	}
	msg := &authn.LoginMsg{}
	if err = cmn.ReadJSON(w, r, msg); err != nil {
		return
	}
	if msg.Password == "" {
		cmn.WriteErrMsg(w, r, "Not authorized", http.StatusUnauthorized)
		return
	}
	userID := apiItems[0]
	pass := msg.Password
	if glog.V(4) {
		glog.Infof("Login user %q", userID)
	}

	tokenString, err := a.users.issueToken(userID, pass, msg.ExpiresIn)
	if err != nil {
		glog.Errorf("Failed to generate token: %v\n", err)
		cmn.WriteErrMsg(w, r, "Not authorized", http.StatusUnauthorized)
		return
	}

	repl := fmt.Sprintf(`{"token": %q}`, tokenString)
	writeBytes(w, []byte(repl), "auth")
}

func writeJSON(w http.ResponseWriter, val interface{}, tag string) {
	w.Header().Set(cmn.HdrContentType, cmn.ContentJSON)
	var err error
	if err = jsoniter.NewEncoder(w).Encode(val); err == nil {
		return
	}
	glog.Errorf("%s: failed to write json, err: %v", tag, err)
}

func writeBytes(w http.ResponseWriter, jsbytes []byte, tag string) {
	w.Header().Set(cmn.HdrContentType, cmn.ContentJSON)
	var err error
	if _, err = w.Write(jsbytes); err == nil {
		return
	}
	glog.Errorf("%s: failed to write json, err: %v", tag, err)
}

func (a *Server) httpSrvPost(w http.ResponseWriter, r *http.Request) {
	if _, err := checkRESTItems(w, r, 0, apc.URLPathClusters.L); err != nil {
		return
	}
	if err := checkAuthorization(w, r); err != nil {
		return
	}
	cluConf := &authn.CluACL{}
	if err := cmn.ReadJSON(w, r, cluConf); err != nil {
		return
	}
	if err := a.users.addCluster(cluConf); err != nil {
		cmn.WriteErr(w, r, err, http.StatusInternalServerError)
		return
	}
}

func (a *Server) httpSrvPut(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathClusters.L)
	if err != nil {
		return
	}
	if err := checkAuthorization(w, r); err != nil {
		return
	}
	cluConf := &authn.CluACL{}
	if err := cmn.ReadJSON(w, r, cluConf); err != nil {
		return
	}
	cluID := apiItems[0]
	if err := a.users.updateCluster(cluID, cluConf); err != nil {
		cmn.WriteErr(w, r, err, http.StatusInternalServerError)
	}
}

func (a *Server) httpSrvDelete(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 0, apc.URLPathClusters.L)
	if err != nil {
		return
	}
	if err = checkAuthorization(w, r); err != nil {
		return
	}

	if len(apiItems) == 0 {
		cmn.WriteErrMsg(w, r, "cluster name or ID is not defined", http.StatusInternalServerError)
		return
	}
	if err := a.users.delCluster(apiItems[0]); err != nil {
		if cmn.IsErrNotFound(err) {
			cmn.WriteErr(w, r, err, http.StatusNotFound)
		} else {
			cmn.WriteErr(w, r, err, http.StatusInternalServerError)
		}
	}
}

func (a *Server) httpSrvGet(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 0, apc.URLPathClusters.L)
	if err != nil {
		return
	}
	var cluList *authn.RegisteredClusters
	if len(apiItems) != 0 {
		cid := apiItems[0]
		clu, err := a.users.getCluster(cid)
		if err != nil {
			if cmn.IsErrNotFound(err) {
				cmn.WriteErr(w, r, err, http.StatusNotFound)
			} else {
				cmn.WriteErr(w, r, err, http.StatusInternalServerError)
			}
			return
		}
		cluList = &authn.RegisteredClusters{
			M: map[string]*authn.CluACL{clu.ID: clu},
		}
	} else {
		clusters, err := a.users.clusterList()
		if err != nil {
			cmn.WriteErr(w, r, err, http.StatusInternalServerError)
			return
		}
		cluList = &authn.RegisteredClusters{M: clusters}
	}
	writeJSON(w, cluList, "auth")
}

func (a *Server) roleHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.httpRolePost(w, r)
	case http.MethodPut:
		a.httpRolePut(w, r)
	case http.MethodDelete:
		a.httpRoleDel(w, r)
	case http.MethodGet:
		a.httpRoleGet(w, r)
	default:
		cmn.WriteErr405(w, r, http.MethodDelete, http.MethodGet, http.MethodPost, http.MethodPut)
	}
}

func (a *Server) httpRoleGet(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 0, apc.URLPathRoles.L)
	if err != nil {
		return
	}
	if len(apiItems) > 1 {
		cmn.WriteErrMsg(w, r, "invalid request")
		return
	}

	if len(apiItems) == 0 {
		roles, err := a.users.roleList()
		if err != nil {
			cmn.WriteErr(w, r, err)
			return
		}
		writeJSON(w, roles, "rolelist")
		return
	}

	role, err := a.users.lookupRole(apiItems[0])
	if err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
	clusters, err := a.users.clusterList()
	if err != nil {
		cmn.WriteErr(w, r, err)
		return
	}
	for _, clu := range role.ClusterACLs {
		if cInfo, ok := clusters[clu.ID]; ok {
			clu.Alias = cInfo.Alias
		}
	}
	writeJSON(w, role, "role")
}

func (a *Server) httpRoleDel(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathRoles.L)
	if err != nil {
		return
	}
	if err = checkAuthorization(w, r); err != nil {
		return
	}

	roleID := apiItems[0]
	if err = a.users.delRole(roleID); err != nil {
		cmn.WriteErr(w, r, err)
	}
}

func (a *Server) httpRolePost(w http.ResponseWriter, r *http.Request) {
	_, err := checkRESTItems(w, r, 0, apc.URLPathRoles.L)
	if err != nil {
		return
	}
	if err = checkAuthorization(w, r); err != nil {
		return
	}
	info := &authn.Role{}
	if err := cmn.ReadJSON(w, r, info); err != nil {
		return
	}
	if err := a.users.addRole(info); err != nil {
		cmn.WriteErrMsg(w, r, fmt.Sprintf("Failed to add role: %v", err), http.StatusInternalServerError)
	}
}

func (a *Server) httpRolePut(w http.ResponseWriter, r *http.Request) {
	apiItems, err := checkRESTItems(w, r, 1, apc.URLPathRoles.L)
	if err != nil {
		return
	}
	if err = checkAuthorization(w, r); err != nil {
		return
	}

	role := apiItems[0]
	updateReq := &authn.Role{}
	err = jsoniter.NewDecoder(r.Body).Decode(updateReq)
	if err != nil {
		cmn.WriteErrMsg(w, r, "Invalid request")
		return
	}
	if glog.V(4) {
		glog.Infof("PUT role %q\n", role)
	}
	if err := a.users.updateRole(role, updateReq); err != nil {
		if cmn.IsErrNotFound(err) {
			cmn.WriteErr(w, r, err, http.StatusNotFound)
		} else {
			cmn.WriteErr(w, r, err)
		}
	}
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpConfigGet(w, r)
	case http.MethodPut:
		httpConfigPut(w, r)
	default:
		cmn.WriteErr405(w, r, http.MethodPut, http.MethodGet)
	}
}

func httpConfigGet(w http.ResponseWriter, r *http.Request) {
	if err := checkAuthorization(w, r); err != nil {
		return
	}
	Conf.RLock()
	defer Conf.RUnlock()
	writeJSON(w, Conf, "config")
}

func httpConfigPut(w http.ResponseWriter, r *http.Request) {
	if err := checkAuthorization(w, r); err != nil {
		return
	}
	updateCfg := &authn.ConfigToUpdate{}
	if err := jsoniter.NewDecoder(r.Body).Decode(updateCfg); err != nil {
		cmn.WriteErrMsg(w, r, "Invalid request")
		return
	}
	if err := Conf.ApplyUpdate(updateCfg); err != nil {
		cmn.WriteErr(w, r, err)
	}
}
