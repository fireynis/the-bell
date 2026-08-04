import { createContext, useContext, useEffect, useState, useCallback } from "react";
import type { ReactNode } from "react";
import type { TownConfig } from "../api/types";
import { configApi } from "../api/client";
import { deriveThemeVars } from "../lib/color";

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

function applyThemeColors(primary?: string, accent?: string) {
  const root = document.documentElement;
  for (const [name, value] of Object.entries(deriveThemeVars(primary, accent))) {
    root.style.setProperty(name, value);
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
