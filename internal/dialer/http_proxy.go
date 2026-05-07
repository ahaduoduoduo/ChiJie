package dialer

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// HTTPProxyDialer HTTP 代理
type HTTPProxyDialer struct {
	node      *Node
	proxyURL  *url.URL
	basicAuth string
}

func NewHTTPProxyDialer(node *Node) (*HTTPProxyDialer, error) {
	proxyURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", node.Server, node.Port),
	}

	var basicAuth string
	if node.Username != "" {
		proxyURL.User = url.UserPassword(node.Username, node.Password)
		basicAuth = "Basic " + base64.StdEncoding.EncodeToString(
			[]byte(node.Username+":"+node.Password),
		)
	}

	return &HTTPProxyDialer{
		node:      node,
		proxyURL:  proxyURL,
		basicAuth: basicAuth,
	}, nil
}

func (d *HTTPProxyDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// 连接到代理服务器
	conn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, d.proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy: %w", err)
	}

	// 发送 CONNECT 请求
	connectReq := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}

	if d.basicAuth != "" {
		connectReq.Header.Set("Proxy-Authorization", d.basicAuth)
	}

	if err := connectReq.Write(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send CONNECT: %w", err)
	}

	// 读取响应
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy returned %d", resp.StatusCode)
	}

	return conn, nil
}

func (d *HTTPProxyDialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyURL(d.proxyURL),
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (d *HTTPProxyDialer) Name() string {
	return fmt.Sprintf("http://%s", d.node.Name)
}
