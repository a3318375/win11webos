package api

import (
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"win11webos/api/internal/mount"
	"win11webos/api/internal/storage"
)

func mountRoutes(v1 *gin.RouterGroup, s *mount.Service) {
	v1.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			if origin := c.GetHeader("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
					fail(c, 403, "origin_rejected", "此开发阶段的挂载管理仅允许本机浏览器操作")
					return
				}
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		}
		c.Next()
	})
	v1.GET("/providers", func(c *gin.Context) {
		providers := storage.Providers()
		out := []gin.H{}
		for _, p := range providers {
			fields := []storage.Field{}
			for _, field := range p.Fields {
				if field.Key != "deleteMode" && field.Key != "downloadMode" {
					fields = append(fields, field)
				}
			}
			out = append(out, gin.H{"id": p.ID, "name": p.Name, "implemented": true, "fields": fields, "capabilities": gin.H{"browse": true, "write": p.ID == "nas" || p.ID == "quark", "download": p.ID == "nas" || p.ID == "quark"}, "automaticAuth": s.AuthAvailable(p.ID), "authHint": "115 / 百度需配置 CLOUD_OAUTH_URL；夸克可直接扫码；也可手动填写凭据"})
		}
		c.JSON(200, gin.H{"data": out})
	})
	v1.GET("/transfers", func(c *gin.Context) { c.JSON(200, gin.H{"data": s.Transfers()}) })
	v1.POST("/transfers", func(c *gin.Context) {
		var in mount.TransferInput
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "invalid_request", "传输参数格式错误")
			return
		}
		task, err := s.CreateTransfer(c.Request.Context(), in)
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(202, gin.H{"data": task})
	})
	v1.POST("/transfers/:task/action", func(c *gin.Context) {
		var in struct {
			Action string `json:"action"`
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "invalid_request", "任务操作格式错误")
			return
		}
		task, err := s.TransferAction(c.Param("task"), in.Action)
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": task})
	})
	v1.GET("/accounts", func(c *gin.Context) {
		data, err := s.Accounts(c.Request.Context())
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": data})
	})
	v1.POST("/accounts/:id/actions", func(c *gin.Context) {
		var in mount.FileAction
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "invalid_request", "文件操作格式错误")
			return
		}
		completed, err := s.FileActions(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			c.JSON(400, gin.H{"error": gin.H{"message": err.Error(), "completed": completed}})
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"completed": completed}})
	})
	v1.GET("/accounts/:id/content", func(c *gin.Context) {
		accounts, accountErr := s.Accounts(c.Request.Context())
		if accountErr != nil {
			mountFailure(c, accountErr)
			return
		}
		for _, a := range accounts {
			if a.ID == c.Param("id") && a.Provider == "quark" {
				response, err := s.DownloadQuark(c.Request.Context(), a.ID, c.Query("parent"), c.Query("id"), c.GetHeader("Range"))
				if err != nil {
					mountFailure(c, err)
					return
				}
				defer response.Body.Close()
				for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition"} {
					if value := response.Header.Get(key); value != "" {
						c.Header(key, value)
					}
				}
				c.Header("X-Content-Type-Options", "nosniff")
				c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
				c.Status(response.StatusCode)
				_, _ = io.Copy(c.Writer, response.Body)
				return
			}
		}
		file, err := s.OpenContent(c.Request.Context(), c.Param("id"), c.Query("id"))
		if err != nil {
			mountFailure(c, err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			mountFailure(c, err)
			return
		}
		extension := strings.ToLower(filepath.Ext(info.Name()))
		safeImage := extension == ".png" || extension == ".jpg" || extension == ".jpeg" || extension == ".webp" || extension == ".gif" || extension == ".avif"
		disposition := "attachment"
		if c.Query("download") != "true" {
			if safeImage {
				disposition = "inline"
				c.Header("Content-Type", mime.TypeByExtension(extension))
			} else if extension == ".txt" || extension == ".md" || extension == ".json" || extension == ".csv" || extension == ".log" {
				disposition = "inline"
				c.Header("Content-Type", "text/plain; charset=utf-8")
			}
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'")
		c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": info.Name()}))
		http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
	})
	decode := func(c *gin.Context) (mount.Input, bool) {
		var in mount.Input
		if err := c.ShouldBindJSON(&in); err != nil {
			fail(c, 400, "invalid_request", "请求格式错误或内容过大")
			return in, false
		}
		return in, true
	}
	v1.POST("/accounts/test", func(c *gin.Context) {
		in, ok := decode(c)
		if !ok {
			return
		}
		normalized, count, err := s.Test(c.Request.Context(), in)
		if err != nil {
			mountFailure(c, err)
			return
		}
		var session *mount.Authorization
		if in.Provider != "nas" {
			auth := s.RetainTest(in.Provider, normalized.Credentials, in.AuthSession)
			session = &auth
		}
		c.JSON(200, gin.H{"data": gin.H{"status": "ok", "count": count, "message": "凭据与根目录读取验证通过", "auth": session}})
	})
	v1.POST("/accounts", func(c *gin.Context) {
		in, ok := decode(c)
		if !ok {
			return
		}
		a, err := s.Create(c.Request.Context(), in)
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(201, gin.H{"data": a})
	})
	v1.DELETE("/accounts/:id", func(c *gin.Context) {
		if err := s.Remove(c.Request.Context(), c.Param("id")); err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"removed": true}})
	})
	v1.GET("/accounts/:id/files", func(c *gin.Context) {
		data, err := s.List(c.Request.Context(), c.Param("id"), c.Query("parent"), c.Query("refresh") == "true")
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": data})
	})
	v1.GET("/local/disks", func(c *gin.Context) { c.JSON(200, gin.H{"data": s.Disks(c.Request.Context())}) })
	v1.GET("/local/directories", func(c *gin.Context) {
		data, err := s.Directories(c.Request.Context(), c.Query("path"))
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": data})
	})
	v1.POST("/auth/sessions", func(c *gin.Context) {
		var in struct {
			Provider string `json:"provider"`
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "invalid_request", "请选择网盘类型")
			return
		}
		data, err := s.StartAuth(c.Request.Context(), in.Provider)
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(201, gin.H{"data": data})
	})
	v1.GET("/auth/sessions/:id", func(c *gin.Context) {
		data, err := s.PollAuth(c.Request.Context(), c.Param("id"))
		if err != nil {
			mountFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": data})
	})
	v1.DELETE("/auth/sessions/:id", func(c *gin.Context) {
		s.CancelAuth(c.Param("id"))
		c.JSON(200, gin.H{"data": gin.H{"cancelled": true}})
	})
}
func mountFailure(c *gin.Context, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, 404, "not_found", "挂载账号不存在")
		return
	}
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "sql") || strings.Contains(message, "constraint") {
		message = "保存挂载配置失败"
	}
	fail(c, 400, "mount_error", message)
}
