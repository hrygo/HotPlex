package com.hotplex.example;

import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * HotPlex OpenCode HTTP Client - Simple Implementation
 *
 * Demonstrates REST + SSE interaction pattern with OpenCode.
 */
public class SimpleClient {
    private static final String BASE_URL = "http://localhost:8080";

    public static void main(String[] args) throws Exception {
        System.out.println("=== HotPlex OpenCode HTTP Client Demo ===\n");

        // 1. Create session
        String sessionId = createSession();
        System.out.println("✅ Session Created: " + sessionId);

        // 2. Start event listener in background
        CountDownLatch latch = new CountDownLatch(1);
        Thread eventListener = new Thread(() -> listenToEvents(sessionId, latch));
        eventListener.start();

        // 3. Send prompt with system prompt
        String prompt = "Write a hello world in Python";
        String systemPrompt = "You are an expert Python developer.";
        sendPrompt(sessionId, prompt, systemPrompt);

        // Wait for completion
        latch.await(60, TimeUnit.SECONDS);
        System.out.println("\n👋 Demo Complete");
        System.exit(0);
    }

    private static String createSession() throws Exception {
        URL url = new URL(BASE_URL + "/session");
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setConnectTimeout(5000);

        if (conn.getResponseCode() != 200) {
            throw new RuntimeException("Failed to create session: " + conn.getResponseCode());
        }

        try (BufferedReader reader = new BufferedReader(
                new InputStreamReader(conn.getInputStream(), StandardCharsets.UTF_8))) {
            String line = reader.readLine();
            // Parse JSON: {"info":{"id":"..."}}
            int idStart = line.indexOf("\"id\":\"") + 5;
            int idEnd = line.indexOf("\"", idStart);
            return line.substring(idStart, idEnd);
        } finally {
            conn.disconnect();
        }
    }

    private static void sendPrompt(String sessionId, String prompt, String systemPrompt) throws Exception {
        URL url = new URL(BASE_URL + "/session/" + sessionId + "/message");
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod("POST");
        conn.setDoOutput(true);
        conn.setRequestProperty("Content-Type", "application/json");

        String json = String.format("{\"prompt\":\"%s\",\"system_prompt\":\"%s\"}",
                escapeJson(prompt), escapeJson(systemPrompt));

        try (OutputStream os = conn.getOutputStream()) {
            os.write(json.getBytes(StandardCharsets.UTF_8));
        }

        if (conn.getResponseCode() != 200) {
            throw new RuntimeException("Failed to send prompt: " + conn.getResponseCode());
        }
        System.out.println("📤 Prompt sent");
        conn.disconnect();
    }

    private static void listenToEvents(String sessionId, CountDownLatch latch) {
        try {
            URL url = new URL(BASE_URL + "/global/event");
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("GET");

            try (BufferedReader reader = new BufferedReader(
                    new InputStreamReader(conn.getInputStream(), StandardCharsets.UTF_8))) {
                System.out.println("📡 Connected to SSE stream...");

                String line;
                while ((line = reader.readLine()) != null) {
                    if (line.startsWith("data: ")) {
                        String data = line.substring(6);
                        // Simple event parsing
                        if (data.contains("\"type\":\"message.part.updated\"")) {
                            if (data.contains("\"text\"")) {
                                int textStart = data.indexOf("\"text\":\"") + 8;
                                int textEnd = data.indexOf("\"", textStart);
                                if (textStart > 7 && textEnd > textStart) {
                                    System.out.print("🤖: " + data.substring(textStart, textEnd));
                                }
                            }
                        }
                        if (data.contains("\"event\":\"completed\"")) {
                            latch.countDown();
                            break;
                        }
                    }
                }
            }
        } catch (Exception e) {
            System.out.println("❌ SSE Error: " + e.getMessage());
            latch.countDown();
        }
    }

    private static String escapeJson(String s) {
        return s.replace("\\", "\\\\")
                .replace("\"", "\\\"")
                .replace("\n", "\\n")
                .replace("\r", "\\r")
                .replace("\t", "\\t");
    }
}
