import sys

def colored(r, g, b, text):
    return f"\033[38;2;{r};{g};{b}m{text}\033[0m"

# HOTPLEX ASCII Art (refining the block style)
# Each letter is 6 lines high.

h = [
    "██╗  ██╗",
    "██║  ██║",
    "███████║",
    "██╔══██║",
    "██║  ██║",
    "╚═╝  ╚═╝"
]

o = [
    " ██████╗ ",
    "██╔═══██╗",
    "██║   ██║",
    "██║   ██║",
    "╚██████╔╝",
    " ╚═════╝ "
]

t = [
    "████████╗",
    "╚══██╔══╝",
    "   ██║   ",
    "   ██║   ",
    "   ██║   ",
    "   ╚═╝   "
]

p = [
    "██████╗ ",
    "██╔══██╗",
    "██████╔╝",
    "██╔═══╝ ",
    "██║     ",
    "╚═╝     "
]

l = [
    "██╗     ",
    "██║     ",
    "██║     ",
    "██║     ",
    "███████╗",
    "╚══════╝"
]

e = [
    "███████╗",
    "██╔════╝",
    "█████╗  ",
    "██╔══╝  ",
    "███████╗",
    "╚══════╝"
]

x = [
    "██╗  ██╗",
    "╚██╗██╔╝",
    " ╚███╔╝ ",
    " ██╔██╗ ",
    "██╔╝ ██╗",
    "╚═╝  ╚═╝"
]

# Colors:
# Hot (Orange): 255, 138, 0
# Plex (Cyan): 0, 185, 203

def get_line(i):
    # HOT (Orange)
    line = colored(255, 138, 0, h[i] + o[i] + t[i])
    # Spacer
    line += "  "
    # PLEX (Cyan)
    line += colored(0, 185, 203, p[i] + l[i] + e[i] + x[i])
    return line

banner = []
for i in range(6):
    banner.append(get_line(i))

# Add a subtitle/tagline without nodes and version
line_sep = colored(100, 100, 100, "─────────────────────────────────────────────────────────────────────")
tagline = "      " + colored(200, 200, 200, "HOTPLEX WORKER GATEWAY")
desc = "      " + colored(120, 120, 120, "Unified AI Coding Agent Access Layer · Multi-Protocol Abstraction")

banner.append("")
banner.append("    " + line_sep)
banner.append(tagline)
banner.append(desc)
banner.append("    " + line_sep)

output = "\n".join(banner)
with open("cmd/worker/banner_art.txt", "w") as f:
    f.write(output)

print("Banner generated successfully.")
