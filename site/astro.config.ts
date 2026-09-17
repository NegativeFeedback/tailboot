import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

export default defineConfig({
  site: "https://negativefeedback.github.io",
  base: "/tailboot",
  // Compression drops the whitespace around inline links and code.
  compressHTML: false,
  vite: {
    plugins: [tailwindcss()],
  },
});
