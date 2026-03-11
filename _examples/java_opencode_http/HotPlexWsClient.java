package com.hotplex.example;

import java.net.URI;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/**
 * HotPlex WebSocket Client - Enterprise Implementation
 *
 * Production-ready client with:
 * - Automatic reconnection with exponential backoff
 * - Comprehensive error handling
 * - Graceful shutdown
 * - Metrics collection
 */
public class HotPlexWsClient {
    private final String url;
    private final String sessionId;
    private final int maxReconnectAttempts;
    private final long baseDelayMs;

    private java.net.http.WebSocket ws;
    private java.net.http.HttpClient httpClient;
    private CountDownLatch connectLatch = new CountDownLatch(1);
    private volatile boolean connected = false;
    private volatile boolean shutdown = false;
    private int reconnectAttempts = 0;

    private StringBuilder answerBuilder = new StringBuilder();
    private Consumer<String> progressCallback;

    // Metrics
    private int requestsTotal = 0;
    private int requestsSuccess = 0;
    private int requestsFailed = 0;
    private long totalLatencyMs = 0;
    private int reconnectCount = 0;

    public HotPlexWsClient(String url, String sessionId) {
        this(url, sessionId, 5, 1000);
    }

    public HotPlexWsClient(String url, String sessionId, int maxReconnectAttempts, long baseDelayMs) {
        this.url = url;
        this.sessionId = sessionId;
        this.maxReconnectAttempts = maxReconnectAttempts;
        this.baseDelayMs = baseDelayMs;
        this.httpClient = java.net.http.HttpClient.newBuilder()
                .connectTimeout(java.time.Duration.ofSeconds(10))
                .build();
    }

    public void setProgressCallback(Consumer<String> callback) {
        this.progressCallback = callback;
    }

    public void connect() throws Exception {
        if (connected) return;

        java.net.http.WebSocket.Builder builder = httpClient.newWebSocketBuilder();
        ws = builder.buildAsync(URI.create(url), new java.net.http.WebSocket.Listener() {
            @Override
            public void onOpen(java.net.http.WebSocket webSocket) {
                connected = true;
                reconnectAttempts = 0;
                System.out.println("✅ Connected to HotPlex");
                connectLatch.countDown();
                webSocket.request(1);
            }

            @Override
            public CompletionStage<?> onText(java.net.http.WebSocket webSocket, CharSequence data, boolean last) {
                handleMessage(data.toString());
                webSocket.request(1);
                return null;
            }

            @Override
            public void onError(java.net.http.WebSocket webSocket, Throwable error) {
                System.out.println("❌ WebSocket error: " + error.getMessage());
                connected = false;
            }

            @Override
            public CompletionStage<?> onClose(java.net.http.WebSocket webSocket, int statusCode, String reason) {
                connected = false;
                System.out.println("🔌 Connection closed: " + statusCode + " " + reason);
                if (!shutdown) {
                    attemptReconnect();
                }
                return null;
            }
        }).get(10, TimeUnit.SECONDS);
    }

    public void disconnect() {
        shutdown = true;
        if (ws != null) {
            ws.sendClose(1000, "Client disconnect");
        }
        connected = false;
    }

    private void attemptReconnect() {
        if (shutdown || reconnectAttempts >= maxReconnectAttempts) {
            System.out.println("❌ Max reconnect attempts reached");
            return;
        }

        reconnectAttempts++;
        reconnectCount++;

        long delay = Math.min(baseDelayMs * (1L << (reconnectAttempts - 1)), 30000);
        System.out.println("🔄 Reconnecting in " + delay + "ms (attempt " + reconnectAttempts + "/" + maxReconnectAttempts + ")");

        try {
            Thread.sleep(delay);
            connect();
        } catch (Exception e) {
            System.out.println("❌ Reconnect failed: " + e.getMessage());
        }
    }

    private void handleMessage(String msg) {
        try {
            if (msg.contains("\"event\":\"thinking\"")) {
                System.out.print(".");
                if (progressCallback != null) progressCallback.accept("thinking");
            } else if (msg.contains("\"event\":\"answer\"")) {
                int dataStart = msg.indexOf("\"event_data\":\"") + 14;
                int dataEnd = msg.indexOf("\"", dataStart);
                if (dataStart > 13 && dataEnd > dataStart) {
                    String answer = msg.substring(dataStart, dataEnd);
                    answerBuilder.append(answer);
                    System.out.print(answer);
                    if (progressCallback != null) progressCallback.accept(answer);
                }
            } else if (msg.contains("\"event\":\"completed\"")) {
                requestsSuccess++;
                System.out.println("\n✅ Task completed");
            } else if (msg.contains("\"event\":\"error\"")) {
                requestsFailed++;
                int msgStart = msg.indexOf("\"message\":\"") + 10;
                int msgEnd = msg.indexOf("\"", msgStart);
                if (msgStart > 9 && msgEnd > msgStart) {
                    System.out.println("❌ Error: " + msg.substring(msgStart, msgEnd));
                }
            }
        } catch (Exception e) {
            System.out.println("⚠️ Parse error: " + e.getMessage());
        }
    }

    public String execute(String prompt) throws Exception {
        return execute(prompt, null);
    }

    public String execute(String prompt, String systemPrompt) throws Exception {
        if (!connected) {
            connect();
        }

        requestsTotal++;
        long startTime = System.currentTimeMillis();
        answerBuilder.setLength(0);

        StringBuilder json = new StringBuilder();
        json.append("{");
        json.append("\"type\":\"execute\",");
        json.append("\"session_id\":\"").append(sessionId).append("\",");
        json.append("\"prompt\":\"").append(escapeJson(prompt)).append("\"");
        if (systemPrompt != null) {
            json.append(",\"system_prompt\":\"").append(escapeJson(systemPrompt)).append("\"");
        }
        json.append("}");

        ws.sendText(json.toString(), true).get(30, TimeUnit.SECONDS);

        // Wait for completion
        synchronized (this) {
            while (connected && answerBuilder.length() == 0) {
                wait(100);
            }
        }

        totalLatencyMs += System.currentTimeMillis() - startTime;
        return answerBuilder.toString();
    }

    public void stopSession() throws Exception {
        if (!connected) return;
        String json = "{\"type\":\"stop\",\"session_id\":\"" + sessionId + "\"}";
        ws.sendText(json, true).get(5, TimeUnit.SECONDS);
    }

    public Metrics getMetrics() {
        return new Metrics(requestsTotal, requestsSuccess, requestsFailed,
                totalLatencyMs, reconnectCount);
    }

    private String escapeJson(String s) {
        return s.replace("\\", "\\\\")
                .replace("\"", "\\\"")
                .replace("\n", "\\n")
                .replace("\r", "\\r");
    }

    public record Metrics(int requestsTotal, int requestsSuccess, int requestsFailed,
                          long totalLatencyMs, int reconnectCount) {
        public long avgLatencyMs() {
            return requestsSuccess > 0 ? totalLatencyMs / requestsSuccess : 0;
        }
    }

    // ==================== Demo ====================

    public static void main(String[] args) throws Exception {
        System.out.println("=== HotPlex WebSocket Client Demo ===\n");

        HotPlexWsClient client = new HotPlexWsClient(
                "ws://localhost:8080/ws/v1/agent",
                "java-enterprise-demo"
        );

        // Graceful shutdown
        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            System.out.println("\n👋 Shutting down...");
            client.disconnect();
            System.out.println("📊 Metrics: " + client.getMetrics());
        }));

        try {
            // Connect
            client.connect();

            // Execute task
            System.out.println("--- Executing Task ---\n");
            String result = client.execute(
                    "Explain what is HotPlex in one paragraph.",
                    "You are a helpful assistant."
            );

            System.out.println("\n\n--- Result ---");
            System.out.println("Length: " + result.length() + " chars");

            // Get metrics
            System.out.println("\n📊 Final Metrics: " + client.getMetrics());

        } catch (Exception e) {
            System.out.println("❌ Demo failed: " + e.getMessage());
            System.out.println("Make sure hotplexd is running on localhost:8080");
        }
    }
}
