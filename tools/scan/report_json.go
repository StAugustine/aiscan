package scan

func formatJSONLines(d *collector) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.jsonLines.String()
}
