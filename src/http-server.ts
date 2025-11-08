import express from 'express';
import cors from 'cors';
import { randomUUID } from 'node:crypto';
import { PostgresMCPServer } from './server.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { InMemoryEventStore } from '@modelcontextprotocol/sdk/examples/shared/inMemoryEventStore.js';
import { SSEServerTransport } from '@modelcontextprotocol/sdk/server/sse.js';
import { KubernetesTokenValidator } from './auth/token-validator.js';
import { getFilteredTools, shouldShowDatabaseTools } from './tools/tool-definitions.js';

interface QueryRequest {
  sql: string;
  parameters?: any[];
  maxRows?: number;
}

interface ToolCallRequest {
  name: string;
  arguments: Record<string, any>;
}

interface SSEConnectionContext {
  response: express.Response;
  showDatabaseTools: boolean;
}

interface MCPTransportContext {
  transport: StreamableHTTPServerTransport;
  showDatabaseTools: boolean;
}

class HTTPMCPServer {
  private app: express.Application;
  private mcpServer: PostgresMCPServer;
  private port: number;
  private transports: Record<string, MCPTransportContext> = {};
  private sseConnections: Record<string, SSEConnectionContext> = {};
  private tokenValidator: KubernetesTokenValidator;

  constructor(databaseUrl: string, port: number = 3000) {
    this.port = port;
    this.mcpServer = new PostgresMCPServer(databaseUrl);
    this.tokenValidator = new KubernetesTokenValidator();
    this.app = express();
    this.setupMiddleware();
    this.setupRoutes();
  }

  private setupMiddleware() {
    this.app.use(cors({
      origin: '*',
      exposedHeaders: ['Mcp-Session-Id']
    }));
    this.app.use(express.json());
    this.app.use(express.urlencoded({ extended: true }));

    // Add authentication middleware
    this.setupAuthMiddleware();
  }

  private setupAuthMiddleware() {
    this.app.use(async (req, res, next) => {
      // Skip authentication for health check endpoint
      if (req.path === '/health') {
        return next();
      }

      console.log(`[AUTH] ${req.method} ${req.path} - Checking authorization`);

      // Check for either standard Authorization header OR custom kubernetes-authorization header
      const standardAuthHeader = req.headers.authorization;
      const customAuthHeader = req.headers['kubernetes-authorization'] as string;

      let authHeader: string;
      let headerSource: string;

      if (standardAuthHeader) {
        authHeader = standardAuthHeader;
        headerSource = 'Authorization';
        console.log('[AUTH] Using standard Authorization header');
      } else if (customAuthHeader) {
        authHeader = customAuthHeader;
        headerSource = 'kubernetes-authorization';
        console.log('[AUTH] Using custom kubernetes-authorization header');
      } else {
        console.log('[AUTH] Missing both Authorization and kubernetes-authorization headers');
        return res.status(401).json({
          error: 'Missing authorization header',
          expected: 'Either "Authorization: Bearer <token>" or "kubernetes-authorization: Bearer <token>"'
        });
      }

      // Validate token format
      if (!authHeader.startsWith('Bearer ')) {
        console.log(`[AUTH] Invalid ${headerSource} header format`);
        return res.status(401).json({
          error: `Invalid ${headerSource} header format`,
          expected: `${headerSource}: Bearer <token>`,
          received: authHeader.substring(0, 20) + '...'
        });
      }

      try {
        // Validate token using Kubernetes TokenReview API
        const validationResult = await this.tokenValidator.validateBearerToken(authHeader);

        if (validationResult.valid) {
          console.log(`[AUTH] Token validated successfully for user: ${validationResult.user?.username} (via ${headerSource} header)`);

          // Check ACM admin permissions
          const hasACMAccess = await this.tokenValidator.checkACMAdminPermissions(validationResult);

          if (hasACMAccess) {
            console.log(`[AUTH] ACM admin access granted for user: ${validationResult.user?.username}`);
            // Store user context and token in request for later use
            (req as any).user = validationResult.user;
            (req as any).token = authHeader.substring(7); // Store token without Bearer prefix
            (req as any).authSource = headerSource; // Track which header was used
            next();
          } else {
            console.log(`[AUTH] ACM admin access denied for user: ${validationResult.user?.username} - insufficient permissions`);
            return res.status(403).json({
              error: 'Access denied',
              details: 'ACM administrator permissions required',
              requirement: 'User must have permissions to create ManagedClusters or be in system:masters group',
              headerUsed: headerSource
            });
          }
        } else {
          console.log(`[AUTH] Token validation failed (via ${headerSource} header): ${validationResult.error}`);
          return res.status(403).json({
            error: 'Token validation failed',
            details: validationResult.error,
            headerUsed: headerSource
          });
        }
      } catch (error) {
        console.error('[AUTH] Token validation error:', error);
        return res.status(500).json({
          error: 'Internal authentication error',
          details: error instanceof Error ? error.message : 'Unknown error'
        });
      }
    });
  }

  private setupRoutes() {
    // Health check endpoint
    this.app.get('/health', (req, res) => {
      res.json({ status: 'ok', timestamp: new Date().toISOString() });
    });

    // MCP Streamable HTTP endpoint (latest protocol)
    this.app.all('/mcp', async (req, res) => {
      console.log(`Received ${req.method} request to /mcp`);

      try {
        const sessionId = req.headers['mcp-session-id'] as string | undefined;
        const dbHeader = req.headers.db as string;
        const showDatabaseTools = shouldShowDatabaseTools(dbHeader);

        console.log(`[MCP] Request with sessionId: ${sessionId}, db header: ${dbHeader}, showDatabaseTools: ${showDatabaseTools}`);

        let transport: StreamableHTTPServerTransport;

        if (sessionId && this.transports[sessionId]) {
          // Reuse existing transport
          transport = this.transports[sessionId].transport;
        } else if (!sessionId && req.method === 'POST') {
          // Create new transport for initialization
          const eventStore = new InMemoryEventStore();
          transport = new StreamableHTTPServerTransport({
            sessionIdGenerator: () => randomUUID(),
            eventStore,
            onsessioninitialized: (sessionId) => {
              console.log(`StreamableHTTP session initialized with ID: ${sessionId}`);
              this.transports[sessionId] = {
                transport,
                showDatabaseTools
              };
            }
          });

          // Set up onclose handler
          transport.onclose = () => {
            const sid = transport.sessionId;
            if (sid && this.transports[sid]) {
              console.log(`Transport closed for session ${sid}, removing from transports map`);
              delete this.transports[sid];
            }
          };

          // Connect the transport to the MCP server with context
          const mcpServer = this.mcpServer.getMcpServer();

          // Store the showDatabaseTools flag in the transport for later use
          (transport as any).showDatabaseTools = showDatabaseTools;

          // Enable/disable database tools based on header context
          const databaseToolNames = ['query_database', 'get_database_stats', 'list_tables', 'search_tables'];

          if (showDatabaseTools) {
            // Enable all database tools
            databaseToolNames.forEach(toolName => {
              const tool = (mcpServer as any)._registeredTools[toolName];
              if (tool) tool.enable();
            });
          } else {
            // Disable database tools, keep only find_resources
            databaseToolNames.forEach(toolName => {
              const tool = (mcpServer as any)._registeredTools[toolName];
              if (tool) tool.disable();
            });
          }

          await mcpServer.connect(transport);
        } else {
          res.status(400).json({
            jsonrpc: '2.0',
            error: {
              code: -32000,
              message: 'Bad Request: Invalid session or request method',
            },
            id: null,
          });
          return;
        }

        // Handle the request
        await transport.handleRequest(req, res);
      } catch (error) {
        console.error('Error handling MCP request:', error);
        if (!res.headersSent) {
          res.status(500).json({ error: 'Internal server error' });
        }
      }
    });

    // Legacy SSE endpoint for backward compatibility
    this.app.get('/sse', (req, res) => {
      const sessionId = Date.now().toString();

      // Capture db header to determine tool visibility
      const dbHeader = req.headers.db as string;
      const showDatabaseTools = shouldShowDatabaseTools(dbHeader);

      console.log(`[SSE] New connection request, creating session: ${sessionId}, db header: ${dbHeader}, showDatabaseTools: ${showDatabaseTools}`);

      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': 'Cache-Control, db'
      });

      // Store the SSE connection with context
      this.sseConnections[sessionId] = {
        response: res,
        showDatabaseTools
      };
      console.log(`[SSE] Stored connection for session: ${sessionId}, total connections: ${Object.keys(this.sseConnections).length}`);

      // Send initial endpoint message (mirroring Neo4j approach)
      res.write(`event: endpoint\ndata: /messages/?session_id=${sessionId}\n\n`);
      console.log(`[SSE] Sent endpoint message for session: ${sessionId}`);

      // Keep connection alive with heartbeat
      const heartbeat = setInterval(() => {
        res.write(`: ping - ${new Date().toISOString()}\n\n`);
      }, 15000);

      req.on('close', () => {
        console.log(`[SSE] Connection closed for session: ${sessionId}`);
        clearInterval(heartbeat);
        delete this.sseConnections[sessionId];
        console.log(`[SSE] Removed connection for session: ${sessionId}, remaining connections: ${Object.keys(this.sseConnections).length}`);
      });
    });

    // Messages endpoint for SSE transport (required by MCP clients)
    this.app.post('/messages/', async (req, res) => {
      const sessionId = req.query.session_id as string;
      
      console.log(`[MESSAGES] Received POST request for session: ${sessionId}`);
      console.log(`[MESSAGES] Request body:`, JSON.stringify(req.body, null, 2));
      console.log(`[MESSAGES] Available sessions:`, Object.keys(this.sseConnections));
      
      if (!sessionId) {
        console.log(`[MESSAGES] Error: No session ID provided`);
        return res.status(400).send('Invalid session ID');
      }

      try {
        // Accept the message immediately (like Neo4j)
        res.status(200).send('Accepted');
        console.log(`[MESSAGES] Message accepted for session: ${sessionId}`);
        
        // Get the SSE connection for this session
        const sseContext = this.sseConnections[sessionId];
        if (!sseContext) {
          console.error(`[MESSAGES] No SSE connection found for session: ${sessionId}`);
          console.error(`[MESSAGES] Available sessions:`, Object.keys(this.sseConnections));
          return;
        }

        const sseRes = sseContext.response;
        console.log(`[MESSAGES] Found SSE connection for session: ${sessionId}, showDatabaseTools: ${sseContext.showDatabaseTools}`);

        // Handle the MCP message
        const message = req.body;
        
        if (message.method === 'initialize') {
          console.log(`[MESSAGES] Handling initialize method`);
          const response = {
            jsonrpc: '2.0',
            id: message.id,
            result: {
              protocolVersion: '2025-06-18',
              capabilities: {
                tools: {
                  listChanged: true
                }
              },
              serverInfo: {
                name: 'postgres-mcp-server',
                version: '1.0.0'
              }
            }
          };
          
          console.log(`[MESSAGES] Sending initialize response:`, JSON.stringify(response, null, 2));
          sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
        } else if (message.method === 'tools/list') {
          console.log(`[MESSAGES] Handling tools/list method, showDatabaseTools: ${sseContext.showDatabaseTools}`);

          // Get filtered tools based on header
          const filteredTools = getFilteredTools(sseContext.showDatabaseTools);

          const response = {
            jsonrpc: '2.0',
            id: message.id,
            result: {
              tools: filteredTools.map(tool => ({
                name: tool.name,
                description: tool.description,
                inputSchema: tool.inputSchema
              }))
            }
          };
          
          console.log(`[MESSAGES] Sending tools/list response:`, JSON.stringify(response, null, 2));
          sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
        } else if (message.method === 'notifications/initialized') {
          console.log(`[MESSAGES] Handling notifications/initialized (ignoring notification)`);
          // This is a notification, not a request, so we don't send a response
        } else if (message.method === 'resources/list') {
          console.log(`[MESSAGES] Handling resources/list method`);
          const response = {
            jsonrpc: '2.0',
            id: message.id,
            result: {
              resources: []
            }
          };
          
          console.log(`[MESSAGES] Sending resources/list response:`, JSON.stringify(response, null, 2));
          sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
        } else if (message.method === 'prompts/list') {
          console.log(`[MESSAGES] Handling prompts/list method`);
          const response = {
            jsonrpc: '2.0',
            id: message.id,
            result: {
              prompts: []
            }
          };
          
          console.log(`[MESSAGES] Sending prompts/list response:`, JSON.stringify(response, null, 2));
          sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
        } else if (message.method === 'tools/call') {
          console.log(`[MESSAGES] Handling tools/call method`);
          console.log(`[MESSAGES] Tool call params:`, JSON.stringify(message.params, null, 2));
          
          try {
            const { name, arguments: args } = message.params;
            const result = await this.mcpServer.callTool(name, args);
            
            // The result should already be in the correct format with content array
            const response = {
              jsonrpc: '2.0',
              id: message.id,
              result: result
            };
            
            console.log(`[MESSAGES] Sending tools/call response:`, JSON.stringify(response, null, 2));
            sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
          } catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error';
            console.error(`[MESSAGES] Error in tools/call:`, errorMessage);
            
            const response = {
              jsonrpc: '2.0',
              id: message.id,
              error: {
                code: -32603,
                message: `Tool execution failed: ${errorMessage}`
              }
            };
            
            console.log(`[MESSAGES] Sending tools/call error response:`, JSON.stringify(response, null, 2));
            sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
          }
        } else {
          console.log(`[MESSAGES] Handling unknown method: ${message.method}`);
          const response = {
            jsonrpc: '2.0',
            id: message.id,
            error: {
              code: -32601,
              message: 'Method not found'
            }
          };
          
          console.log(`[MESSAGES] Sending error response:`, JSON.stringify(response, null, 2));
          sseRes.write(`event: message\ndata: ${JSON.stringify(response)}\n\n`);
        }
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Unknown error';
        console.error(`[MESSAGES] Error handling message:`, errorMessage);
      }
    });

    // Legacy query endpoint for backward compatibility
    this.app.post('/sse/query', async (req, res) => {
      const { sql, parameters, maxRows }: QueryRequest = req.body;

      if (!sql) {
        return res.status(400).json({ error: 'SQL query is required' });
      }

      // Check if direct SQL queries are allowed based on db header
      const dbHeader = req.headers.db as string;
      const showDatabaseTools = shouldShowDatabaseTools(dbHeader);

      console.log(`[LEGACY-QUERY] SQL query request, db header: ${dbHeader}, showDatabaseTools: ${showDatabaseTools}`);

      if (!showDatabaseTools) {
        console.log(`[LEGACY-QUERY] Direct SQL access blocked - requires 'db: show' header`);
        res.status(403).json({
          error: 'Direct SQL access not available',
          details: 'Direct SQL queries require database access. Add \'db: show\' header to execute SQL queries.',
          alternative: 'Use find_resources tool for resource queries without the db header'
        });
        return;
      }

      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': 'Cache-Control, db'
      });

      try {
        const results = await this.mcpServer.executeQuery(sql, parameters, { maxRows });

        res.write(`event: results\ndata: ${JSON.stringify(results)}\n\n`);
        res.write(`event: complete\ndata: success\n\n`);
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Unknown error';
        res.write(`event: error\ndata: ${errorMessage}\n\n`);
      } finally {
        res.end();
      }
    });

    // Legacy tool endpoint for backward compatibility
    this.app.post('/sse/tools/:toolName', async (req, res) => {
      const { toolName } = req.params;
      const { arguments: args }: ToolCallRequest = req.body;

      // Check if tool is allowed based on db header
      const dbHeader = req.headers.db as string;
      const showDatabaseTools = shouldShowDatabaseTools(dbHeader);
      const filteredTools = getFilteredTools(showDatabaseTools);
      const allowedToolNames = filteredTools.map(tool => tool.name);

      console.log(`[LEGACY-TOOL] Request for tool: ${toolName}, db header: ${dbHeader}, showDatabaseTools: ${showDatabaseTools}`);

      if (!allowedToolNames.includes(toolName)) {
        console.log(`[LEGACY-TOOL] Tool '${toolName}' blocked - not in allowed tools: ${allowedToolNames.join(', ')}`);
        res.status(403).json({
          error: 'Tool not available',
          details: `Tool '${toolName}' requires database access. Add 'db: show' header to access database tools.`,
          availableTools: allowedToolNames
        });
        return;
      }

      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': 'Cache-Control, db'
      });

      try {
        const result = await this.mcpServer.callTool(toolName, args);

        res.write(`event: results\ndata: ${JSON.stringify(result)}\n\n`);
        res.write(`event: complete\ndata: success\n\n`);
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Unknown error';
        res.write(`event: error\ndata: ${errorMessage}\n\n`);
      } finally {
        res.end();
      }
    });

    // List available tools
    this.app.get('/tools', (req, res) => {
      const dbHeader = req.headers.db as string;
      const showDatabaseTools = shouldShowDatabaseTools(dbHeader);
      const filteredTools = getFilteredTools(showDatabaseTools);
      const toolNames = filteredTools.map(tool => tool.name);

      console.log(`[TOOLS] Request with db header: ${dbHeader}, showDatabaseTools: ${showDatabaseTools}, returning ${toolNames.length} tools`);
      res.json({ tools: toolNames });
    });

    // Get database statistics
    this.app.get('/stats', async (req, res) => {
      try {
        const stats = await this.mcpServer.getDatabaseStats();
        res.json(stats);
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Unknown error';
        res.status(500).json({ error: errorMessage });
      }
    });

    // List tables
    this.app.get('/tables', async (req, res) => {
      try {
        const tables = await this.mcpServer.listTables();
        res.json({ tables });
      } catch (error) {
        const errorMessage = error instanceof Error ? error.message : 'Unknown error';
        res.status(500).json({ error: errorMessage });
      }
    });

    // MCP server info endpoint
    this.app.get('/info', (req, res) => {
      res.json({
        name: 'PostgreSQL MCP Server',
        version: '1.0.0',
        description: 'MCP server providing PostgreSQL database access',
        capabilities: {
          streamableHttp: true,
          sse: true, // legacy support
          tools: this.mcpServer.getAvailableTools()
        },
        endpoints: {
          mcp: '/mcp', // Latest Streamable HTTP transport
          sse: '/sse', // Legacy SSE endpoint
          query: '/sse/query', // Legacy query endpoint
          tools: '/sse/tools/:toolName', // Legacy tool endpoint
          health: '/health',
          info: '/info'
        },
        protocol: {
          primary: 'streamable-http-2025-03-26',
          legacy: 'sse-2024-11-05'
        }
      });
    });
  }

  async start() {
    try {
      // Test database connection
      const isConnected = await this.mcpServer.testConnection();
      if (!isConnected) {
        console.error('Failed to connect to PostgreSQL database. Please check your configuration.');
        process.exit(1);
      }

      this.app.listen(this.port, () => {
        console.error(`HTTP MCP Server running on http://localhost:${this.port}`);
        console.error('Available endpoints:');
        console.error('  GET/POST/DELETE /mcp - MCP Streamable HTTP (latest)');
        console.error('  GET  /sse        - Legacy SSE endpoint');
        console.error('  POST /sse/query  - Legacy SQL query (SSE)');
        console.error('  POST /sse/tools/:name - Legacy tool calls (SSE)');
        console.error('  GET  /tools      - List available tools');
        console.error('  GET  /stats      - Database statistics');
        console.error('  GET  /tables     - List tables');
        console.error('  GET  /info       - Server info');
        console.error('  GET  /health     - Health check');
      });
    } catch (error) {
      console.error('Failed to start HTTP server:', error);
      process.exit(1);
    }
  }
}

// CLI interface
async function main() {
  // Read from environment variables with fallbacks
  const databaseUrl = process.env.DATABASE_URL || process.argv[2];
  const port = parseInt(process.env.PORT || process.argv[3] || '3000');

  if (!databaseUrl) {
    console.error('Usage: node http-server.js <database-url> [port]');
    console.error('Or set environment variables:');
    console.error('  DATABASE_URL=postgresql://user:pass@localhost:5432/db');
    console.error('  PORT=3000');
    console.error('');
    console.error('Examples:');
    console.error('  node http-server.js postgresql://user:pass@localhost:5432/db 3000');
    console.error('  DATABASE_URL=postgresql://user:pass@localhost:5432/db PORT=3000 node http-server.js');
    process.exit(1);
  }

  const server = new HTTPMCPServer(databaseUrl, port);
  await server.start();
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch(console.error);
}

export { HTTPMCPServer }; 