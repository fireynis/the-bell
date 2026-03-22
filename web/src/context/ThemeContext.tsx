import { createContext, useContext, useEffect, useState, useCallback } from "react";
import type { ReactNode } from "react";
import type { TownConfig } from "../api/types";
import { configApi } from "../api/client";

interface ThemeContextValue {
  config: TownConfig;
  loading: boolean;
  updateConfig: (updates: Record<string, string>) => Promise<void>;
}

const defaults: TownConfig = {
  town_name: "The Bell",
};

const ThemeContext = createContext<ThemeContextValue>({
  config: defaults,
  loading: true,
  updateConfig: async () => {},
});

function hexToHSL(hex: string): { h: number; s: number; l: number } | null {
  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  if (!result) return null;
  const r = parseInt(result[1], 16) / 255;
  const g = parseInt(result[2], 16) / 255;
  const b = parseInt(result[3], 16) / 255;
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  let h = 0, s = 0;
  const l = (max + min) / 2;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)) / 6; break;
      case g: h = ((b - r) / d + 2) / 6; break;
      case b: h = ((r - g) / d + 4) / 6; break;
    }
  }
  return { h: Math.round(h * 360), s: Math.round(s * 100), l: Math.round(l * 100) };
}

function applyThemeColors(primary?: string, accent?: string) {
  const root = document.documentElement;
  if (primary) {
    const hsl = hexToHSL(primary);
    if (hsl) {
      root.style.setProperty("--color-primary", primary);
      root.style.setProperty("--color-primary-hover", `hsl(${hsl.h}, ${hsl.s}%, ${Math.max(0, hsl.l - 10)}%)`);
      root.style.setProperty("--color-primary-light", `hsl(${hsl.h}, ${Math.min(100, hsl.s)}%, 94%)`);
      root.style.setProperty("--color-primary-subtle", `hsl(${hsl.h}, ${Math.min(100, hsl.s)}%, 97%)`);
    }
  }
  if (accent) {
    const hsl = hexToHSL(accent);
    if (hsl) {
      root.style.setProperty("--color-accent", accent);
      root.style.setProperty("--color-accent-hover", `hsl(${hsl.h}, ${hsl.s}%, ${Math.max(0, hsl.l - 8)}%)`);
      root.style.setProperty("--color-accent-light", `hsl(${hsl.h}, ${Math.min(100, hsl.s)}%, 94%)`);
    }
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<TownConfig>(defaults);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    configApi.getConfig()
      .then((cfg) => {
        setConfig({ ...defaults, ...cfg });
        applyThemeColors(cfg.primary_color, cfg.accent_color);
      })
      .catch(() => {
        // Fall back to defaults silently
      })
      .finally(() => setLoading(false));
  }, []);

  const updateConfig = useCallback(async (updates: Record<string, string>) => {
    await configApi.updateConfig(updates);
    setConfig((prev) => {
      const next = { ...prev, ...updates };
      applyThemeColors(next.primary_color, next.accent_color);
      return next;
    });
  }, []);

  return (
    <ThemeContext value={{ config, loading, updateConfig }}>
      {children}
    </ThemeContext>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext);
}
