// Package api provides the HTTP API server and WebSocket endpoint for InfraMap.
package api

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/inframap/inframap/internal/cost"
	"github.com/inframap/inframap/internal/doctor"
	"github.com/inframap/inframap/internal/export"
	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
	"github.com/inframap/inframap/internal/snapshot"
)

// Server is the HTTP API server for InfraMap.
type Server struct {
	registry  *provider.Registry
	graph     *graph.Graph
	findings  []doctor.Finding
	port      int
	staticFS  embed.FS
	wsClients map[*websocket.Conn]bool
	wsMu      sync.Mutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// NewServer creates a new API server.
func NewServer(registry *provider.Registry, staticFS embed.FS, port int) *Server {
	return &Server{
		registry:  registry,
		graph:     graph.New(),
		port:      port,
		staticFS:  staticFS,
		wsClients: make(map[*websocket.Conn]bool),
	}
}

// Run starts the server: runs discovery, then serves the API.
func (s *Server) Run(ctx context.Context, providers []string) error {
	// Run discovery
	log.Println("🔍 Discovering infrastructure...")
	g, err := s.registry.DiscoverAll(ctx, providers)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	s.graph = g
	s.postProcess()

	stats := g.Stats()
	log.Printf("✅ Discovered %d nodes and %d edges", stats.TotalNodes, stats.TotalEdges)
	log.Printf("🩺 Doctor: %d findings · 💰 est. $%.0f/mo", len(s.findings), stats.TotalMonthlyCost)
	for prov, count := range stats.NodesByProvider {
		log.Printf("   📦 %s: %d resources", prov, count)
	}

	// Set up Gin
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		AllowCredentials: true,
	}))

	// API routes
	api := router.Group("/api")
	{
		api.GET("/graph", s.handleGetGraph)
		api.GET("/graph/nodes", s.handleGetNodes)
		api.GET("/graph/nodes/:id", s.handleGetNode)
		api.GET("/graph/edges", s.handleGetEdges)
		api.GET("/graph/stats", s.handleGetStats)
		api.GET("/graph/search", s.handleSearch)
		api.GET("/providers", s.handleGetProviders)
		api.POST("/discover", s.handleDiscover(ctx, providers))
		api.GET("/findings", s.handleGetFindings)
		api.GET("/snapshots", s.handleListSnapshots)
		api.GET("/snapshots/diff", s.handleSnapshotDiff)
		api.GET("/export/terraform", s.handleExportTerraform)
	}

	// WebSocket
	router.GET("/ws", s.handleWebSocket)

	// Serve embedded static files (frontend)
	staticSub, err := fs.Sub(s.staticFS, "web/static")
	if err != nil {
		return fmt.Errorf("failed to load static files: %w", err)
	}
	router.NoRoute(gin.WrapH(http.FileServer(http.FS(staticSub))))

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🌐 InfraMap dashboard: http://localhost:%d", s.port)
	log.Printf("📡 API endpoint:       http://localhost:%d/api/graph", s.port)
	log.Printf("🔌 WebSocket:          ws://localhost:%d/ws", s.port)
	log.Println("Press Ctrl+C to stop")

	srv := &http.Server{Addr: addr, Handler: router}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	return srv.ListenAndServe()
}

// ─── Handlers ───────────────────────────────────────────

func (s *Server) handleGetGraph(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"nodes": s.graph.NodeSlice(),
		"edges": s.graph.EdgeSlice(),
		"stats": s.graph.Stats(),
	})
}

func (s *Server) handleGetNodes(c *gin.Context) {
	prov := c.Query("provider")
	typ := c.Query("type")
	status := c.Query("status")

	if prov != "" || typ != "" || status != "" {
		c.JSON(http.StatusOK, s.graph.FilterNodes(prov, typ, status))
		return
	}

	c.JSON(http.StatusOK, s.graph.NodeSlice())
}

func (s *Server) handleGetNode(c *gin.Context) {
	id := c.Param("id")
	node := s.graph.GetNode(id)
	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}

	// Include connected edges
	edgesFrom := s.graph.GetEdgesFrom(id)
	edgesTo := s.graph.GetEdgesTo(id)
	children := s.graph.GetChildren(id)

	c.JSON(http.StatusOK, gin.H{
		"node":       node,
		"edges_from": edgesFrom,
		"edges_to":   edgesTo,
		"children":   children,
	})
}

func (s *Server) handleGetEdges(c *gin.Context) {
	c.JSON(http.StatusOK, s.graph.EdgeSlice())
}

func (s *Server) handleGetStats(c *gin.Context) {
	c.JSON(http.StatusOK, s.graph.Stats())
}

func (s *Server) handleSearch(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	c.JSON(http.StatusOK, s.graph.Search(q))
}

func (s *Server) handleGetProviders(c *gin.Context) {
	c.JSON(http.StatusOK, s.registry.List())
}

func (s *Server) handleDiscover(ctx context.Context, providers []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		g, err := s.registry.DiscoverAll(ctx, providers)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		s.graph = g
		s.postProcess()

		// Notify WebSocket clients
		s.broadcastRefresh()

		c.JSON(http.StatusOK, gin.H{
			"message": "discovery complete",
			"stats":   g.Stats(),
		})
	}
}

// postProcess runs after every discovery: cost annotation, doctor scan, snapshot.
func (s *Server) postProcess() {
	cost.Annotate(s.graph)
	s.findings = doctor.Scan(s.graph)
	if err := snapshot.Save(s.graph); err != nil {
		log.Printf("⚠️  snapshot save failed: %v", err)
	}
}

func (s *Server) handleGetFindings(c *gin.Context) {
	c.JSON(http.StatusOK, s.findings)
}

func (s *Server) handleListSnapshots(c *gin.Context) {
	metas, err := snapshot.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metas)
}

func (s *Server) handleSnapshotDiff(c *gin.Context) {
	file := c.Query("file")
	if file == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'file' is required"})
		return
	}
	d, err := snapshot.DiffCurrent(s.graph, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, d)
}

func (s *Server) handleExportTerraform(c *gin.Context) {
	tf, err := export.Export(s.graph, export.FormatTerraform)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="main.tf"`)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(tf))
}

// ─── WebSocket ──────────────────────────────────────────

func (s *Server) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	s.wsMu.Lock()
	s.wsClients[conn] = true
	s.wsMu.Unlock()

	// Send initial graph
	conn.WriteJSON(gin.H{
		"type":  "full_graph",
		"nodes": s.graph.NodeSlice(),
		"edges": s.graph.EdgeSlice(),
		"stats": s.graph.Stats(),
	})

	// Keep connection alive and listen for pings
	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, conn)
		s.wsMu.Unlock()
		conn.Close()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (s *Server) broadcastRefresh() {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	msg := gin.H{
		"type":  "full_graph",
		"nodes": s.graph.NodeSlice(),
		"edges": s.graph.EdgeSlice(),
		"stats": s.graph.Stats(),
	}

	for conn := range s.wsClients {
		if err := conn.WriteJSON(msg); err != nil {
			conn.Close()
			delete(s.wsClients, conn)
		}
	}
}
