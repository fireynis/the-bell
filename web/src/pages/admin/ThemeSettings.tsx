import { useState } from "react";
import { useTheme } from "../../context/ThemeContext";

const DEFAULT_PRIMARY = "#0A7AFF";
const DEFAULT_ACCENT = "#D4A017";

export default function ThemeSettings() {
  const { config, updateConfig } = useTheme();

  const [townName, setTownName] = useState(config.town_name || "");
  const [primaryColor, setPrimaryColor] = useState(config.primary_color || DEFAULT_PRIMARY);
  const [accentColor, setAccentColor] = useState(config.accent_color || DEFAULT_ACCENT);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  async function handleSave() {
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      const updates: Record<string, string> = {};
      if (townName.trim()) updates.town_name = townName.trim();
      if (primaryColor) updates.primary_color = primaryColor;
      if (accentColor) updates.accent_color = accentColor;
      await updateConfig(updates);
      setSuccess(true);
    } catch {
      setError("Failed to save theme settings.");
    } finally {
      setSaving(false);
    }
  }

  function handleReset() {
    setPrimaryColor(DEFAULT_PRIMARY);
    setAccentColor(DEFAULT_ACCENT);
  }

  return (
    <div
      className="p-6"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
    >
      <h2
        className="mb-4 text-lg font-semibold"
        style={{ color: "var(--color-text)" }}
      >
        Town Appearance
      </h2>

      <div className="space-y-4">
        {/* Town Name */}
        <div>
          <label className="mb-1 block text-sm font-medium" style={{ color: "var(--color-text-secondary)" }}>
            Town Name
          </label>
          <input
            value={townName}
            onChange={(e) => setTownName(e.target.value)}
            maxLength={100}
            className="w-full px-3 py-2 text-sm focus:outline-none"
            style={{
              borderWidth: "1px",
              borderStyle: "solid",
              borderColor: "var(--color-border)",
              borderRadius: "var(--radius-sm)",
              color: "var(--color-text)",
              backgroundColor: "var(--color-surface)",
            }}
            onFocus={(e) => {
              e.currentTarget.style.borderColor = "var(--color-primary)";
              e.currentTarget.style.boxShadow = "0 0 0 1px var(--color-primary)";
            }}
            onBlur={(e) => {
              e.currentTarget.style.borderColor = "var(--color-border)";
              e.currentTarget.style.boxShadow = "none";
            }}
          />
        </div>

        {/* Colors */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="mb-1 block text-sm font-medium" style={{ color: "var(--color-text-secondary)" }}>
              Primary Color
            </label>
            <div className="flex items-center gap-2">
              <input
                type="color"
                value={primaryColor}
                onChange={(e) => setPrimaryColor(e.target.value)}
                className="h-9 w-9 cursor-pointer rounded border-0 bg-transparent p-0"
              />
              <span className="text-xs font-mono" style={{ color: "var(--color-text-tertiary)" }}>
                {primaryColor}
              </span>
            </div>
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium" style={{ color: "var(--color-text-secondary)" }}>
              Accent Color
            </label>
            <div className="flex items-center gap-2">
              <input
                type="color"
                value={accentColor}
                onChange={(e) => setAccentColor(e.target.value)}
                className="h-9 w-9 cursor-pointer rounded border-0 bg-transparent p-0"
              />
              <span className="text-xs font-mono" style={{ color: "var(--color-text-tertiary)" }}>
                {accentColor}
              </span>
            </div>
          </div>
        </div>

        {/* Preview swatches */}
        <div>
          <p className="mb-2 text-xs font-medium" style={{ color: "var(--color-text-tertiary)" }}>Preview</p>
          <div className="flex gap-2">
            <div className="h-8 w-16 rounded" style={{ backgroundColor: primaryColor }} />
            <div className="h-8 w-16 rounded" style={{ backgroundColor: accentColor }} />
          </div>
        </div>

        {/* Error / Success */}
        {error && (
          <div className="text-sm" style={{ color: "var(--color-danger)" }}>{error}</div>
        )}
        {success && (
          <div
            className="rounded-[var(--radius-md)] p-3 text-sm"
            style={{ backgroundColor: "var(--color-success-light)", color: "var(--color-success)" }}
          >
            Theme updated successfully.
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-2">
          <button
            onClick={handleSave}
            disabled={saving}
            className="rounded-full px-4 py-2 text-sm font-semibold disabled:opacity-50"
            style={{
              backgroundColor: "var(--color-primary)",
              color: "var(--color-text-inverse)",
            }}
          >
            {saving ? "Saving..." : "Save"}
          </button>
          <button
            onClick={handleReset}
            className="rounded-full px-4 py-2 text-sm font-medium"
            style={{
              backgroundColor: "var(--color-surface-tertiary)",
              color: "var(--color-text-secondary)",
            }}
          >
            Reset to defaults
          </button>
        </div>
      </div>
    </div>
  );
}
