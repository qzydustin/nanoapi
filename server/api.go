package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func parseTimeRange(c *gin.Context) (time.Time, time.Time) {
	var from, to time.Time
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	return from, to
}

// UsageSummaryHandler handles GET /api/usage for the calling token.
func UsageSummaryHandler(usageSvc *UsageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tc := getTokenContext(c)
		if tc == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing token context"}})
			return
		}

		from, to := parseTimeRange(c)
		s, err := usageSvc.SummaryUsage(tc.TokenID, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token_id": tc.TokenID,
			"summary":  s,
		})
	}
}

// UsageLogsHandler handles GET /api/logs for the calling token.
func UsageLogsHandler(usageSvc *UsageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tc := getTokenContext(c)
		if tc == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing token context"}})
			return
		}

		from, to := parseTimeRange(c)
		records, err := usageSvc.QueryUsage(tc.TokenID, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token_id": tc.TokenID,
			"records":  records,
		})
	}
}
