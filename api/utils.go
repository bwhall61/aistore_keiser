// Package api provides RESTful API to DFC object storage
/*
 * Copyright (c) 2018, NVIDIA CORPORATION. All rights reserved.
 */
package api

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/NVIDIA/dfcpub/memsys"
)

var (
	Mem2 *memsys.Mem2
)

// ParamsOptional is used in constructing client-side API requests to the DFC backend.
// Stores Query and Headers for providing arguments that are not used commonly in API requests
type ParamsOptional struct {
	Query   url.Values
	Headers map[string]string
}

func init() {
	Mem2 = memsys.Init()
}

// DoHTTPRequest sends one HTTP request and returns only the body of the response
func DoHTTPRequest(httpClient *http.Client, method, url string, b []byte, optParams ...ParamsOptional) ([]byte, error) {
	resp, err := doHTTPRequestGetResp(httpClient, method, url, b, optParams...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

// doHTTPRequestGetResp sends one HTTP request and returns the whole response
func doHTTPRequestGetResp(httpClient *http.Client, method, reqURL string, b []byte, optParams ...ParamsOptional) (*http.Response, error) {
	var (
		reqBody io.Reader
	)
	if b != nil {
		reqBody = bytes.NewBuffer(b)
	}
	req, err := http.NewRequest(method, reqURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("Failed to create request, err: %v", err)
	}
	if len(optParams) > 0 {
		setRequestOptParams(req, optParams[0])
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to %s, err: %v", method, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("Failed to read response, err: %v", err)
		}
		return nil, fmt.Errorf("HTTP error = %d, message = %s", resp.StatusCode, string(b))
	}
	return resp, nil
}

// Given an existing HTTP Request and optional API parameters, setRequestOptParams
// sets the optional fields of the request if provided
func setRequestOptParams(req *http.Request, optParams ParamsOptional) {
	if len(optParams.Query) != 0 {
		req.URL.RawQuery = optParams.Query.Encode()
	}
	for k, v := range optParams.Headers {
		req.Header.Set(k, v)
	}
}

func getObjectOptParams(options GetObjectInput) (w io.Writer, q url.Values) {
	w = ioutil.Discard
	if options.Writer != nil {
		w = options.Writer
	}
	if len(options.Query) != 0 {
		q = options.Query
	}
	return
}
