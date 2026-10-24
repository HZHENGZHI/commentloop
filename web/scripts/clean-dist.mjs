// Reset the Vite output directory while keeping the tracked placeholder that
// lets `go build` succeed before the frontend has ever been built.
import { mkdir, rm, writeFile } from "node:fs/promises";

const dist = new URL("../dist/", import.meta.url);
await rm(dist, { recursive: true, force: true });
await mkdir(dist, { recursive: true });
await writeFile(new URL(".gitkeep", dist), "");
