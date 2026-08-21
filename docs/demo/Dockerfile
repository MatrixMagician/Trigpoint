FROM ghcr.io/charmbracelet/vhs:latest
RUN apt-get update && apt-get install -y --no-install-recommends tmux && rm -rf /var/lib/apt/lists/*
COPY trig /usr/local/bin/trig
