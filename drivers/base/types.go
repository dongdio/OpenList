package base

import "resty.dev/v3"

type Json map[string]any

type TokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type ReqCallback func(req *resty.Request)