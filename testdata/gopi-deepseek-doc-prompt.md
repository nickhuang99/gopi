Analyze the DeepSeek API docs at https://api-docs.deepseek.com/zh-cn/quick_start/agent_integrations/pi_mono to find optimization opportunities for our GoPi agent.

Step 1: Use bash curl to download the doc page.
  bash("curl -sL 'https://api-docs.deepseek.com/zh-cn/quick_start/agent_integrations/pi_mono' -o /tmp/deepseek-pi-doc.html")

Step 2: Use read to extract the key sections. The page is Docusaurus HTML — look for the <article> section which contains the actual content. Use bash to strip HTML tags:
  bash("grep -oP '(?<=<p>).*?(?=</p>)' /tmp/deepseek-pi-doc.html | head -50")
  bash("grep -oP '<h[12][^>]*>.*?</h[12]>' /tmp/deepseek-pi-doc.html")

Step 3: Read the extracted content, then compare with our current GoPi implementation at /home/user/work/GoPi/:

   Current GoPi features:
   - 4 tools: read, write, edit, bash
   - DeepSeek v4-pro via api.deepseek.com/v1/chat/completions
   - ReAct loop with tool call execution
   - Static binary, zero deps
   - Debug logging with GOPI_DEBUG=1

   Compare against the Pi integration doc. Find what we're MISSING:
   - Does the doc mention streaming SSE support?
   - Does it mention token counting or cost tracking?
   - Does it mention context compression?
   - Does it mention retry strategies or error handling?
   - Does it mention the models.json format for provider configuration?

Step 4: Write a detailed analysis to /tmp/gopi-optimization-report.md with:
   - What Pi does that GoPi should add
   - What GoPi does better (static binary, zero deps, simpler)
   - Priority-ranked optimization recommendations
   - Estimated effort for each (small/medium/large)

Step 5: Use bash to verify the report is well-formed:
  bash("wc -l /tmp/gopi-optimization-report.md && head -20 /tmp/gopi-optimization-report.md")
