package limits

// What the download limits do with a download handed to a signed link of the object store (usage-and-limits.md R-LIM-040,
// storage.md R-STOR-070 step 10): the transfer runs between the client and the store, so no counting writer sees its bytes and no
// lease can last as long as it does. No figure keeps a download from a link: a speed limit (cachedBytesPerSecond) paces the bytes
// Quader writes and a link's transfer writes none, in enforce and in audit alike (Q-LIM-99, which supersedes Q-LIM-97). The hour
// and day budgets hold through the check before the download (Admit) and the charge at issuance below.

// ChargeLink charges a download handed to a link with the item's size when the link is issued (Q-LIM-98): as cached bytes of the
// budget key, which reach the hour and day windows at the next flush, the usage store under the repository that served and
// bytes_cached of the request log, as if the body had been written. A link fetched twice counts once, a link never fetched counts.
// The parallel lease that Admit took ends with the request, the redirect.
func (c *Charge) ChargeLink(size int64) {
	c.AddCached(int(size))
}
