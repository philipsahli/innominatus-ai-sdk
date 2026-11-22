# RAG Demo

Adds three platform-engineering docs to the in-memory RAG store (OpenAI embeddings), then asks Claude three questions with the top matching docs as context.

Separate Go module (`example.com/rag-demo`); `go.mod` replaces the SDK with `../..`, so it builds against your checkout.

## Run

```bash
git clone https://github.com/philipsahli/innominatus-ai-sdk
cd innominatus-ai-sdk/examples/rag-demo
export ANTHROPIC_API_KEY="your-anthropic-key"
export OPENAI_API_KEY="your-openai-key"
go run .
```

Build only: `go build` (binary `rag-demo`).

Without both keys it exits with `ANTHROPIC_API_KEY environment variable is required` (or `OPENAI_API_KEY ...`).

## Output

Illustrative, shortened; not from a real run. Scores and answers vary.

```
=================================================================
RAG Demo - AI Conversation with Document Context
=================================================================

📚 Step 1: Adding documents to knowledge base...

✅ Successfully added 3 documents to knowledge base

💬 Step 2: Asking AI questions using RAG context...

❓ Question 1: What CPU and memory limits should I use for a medium-sized web service in Kubernetes?

📖 Retrieved 2 relevant documents (showing relevance scores):
   1. Kubernetes Resource Best Practices (0.62 relevance)
   2. Monitoring and Observability (0.34 relevance)

🤖 AI Response:
For a medium-sized web service: CPU 500m-1000m, memory 1Gi-2Gi ...

📊 Tokens used: 812 (prompt: 640, completion: 172)

-----------------------------------------------------------------
...
=================================================================
✅ RAG Demo Complete!
=================================================================
```
