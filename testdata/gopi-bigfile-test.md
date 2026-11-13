Find and analyze a specific diary entry in a large HTML file.

Step 1: Check file size and line count.
  bash("wc -l ~/diabloforum/2021.htm")

Step 2: The file has ~20000+ lines. You CANNOT read it all — too large.
  Instead, find where the "TL;DR" section ends and the diary content begins.
  Use bash grep to find the line number of the relevant section:
  bash("grep -n 'TL;DR\|2021-' ~/diabloforum/2021.htm | head -10")

Step 3: Read ONLY the first 100 lines to understand the structure:
  read("~/diabloforum/2021.htm", offset=0, limit=100)

Step 4: Use the line numbers from Step 2 to read ONLY the relevant diary portion.
  Read a small window around the TL;DR section:
  read("~/diabloforum/2021.htm", offset=80, limit=80)

Step 5: Based on what you find, answer: What is the main topic of the 2021 diary?
  Write a 2-3 sentence summary to /tmp/2021-diary-summary.md.

CRITICAL: Use offset and limit for EVERY read. The file is 20591 lines — reading it all would explode context.
After writing the summary, print to stdout what the diary is about.
