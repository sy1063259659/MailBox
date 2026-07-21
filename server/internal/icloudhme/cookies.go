package icloudhme

// GetCookies returns a copy of the current session cookies, including cookies
// rotated by Apple responses.
func (c *Client) GetCookies() map[string]string {
	out := make(map[string]string, len(c.Cookies))
	for name, value := range c.Cookies {
		out[name] = value
	}
	return out
}
