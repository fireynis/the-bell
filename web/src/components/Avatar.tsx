interface AvatarProps {
  url: string;
  name: string;
  size?: "sm" | "md" | "lg";
}

const sizeMap = {
  sm: "h-8 w-8 text-xs",
  md: "h-10 w-10 text-sm",
  lg: "h-14 w-14 text-lg",
};

export default function Avatar({ url, name, size = "md" }: AvatarProps) {
  const cls = sizeMap[size];
  if (url) {
    return <img src={url} alt={name} className={`${cls} rounded-full object-cover`} />;
  }
  const initial = (name || "?").charAt(0).toUpperCase();
  return (
    <div
      className={`${cls} flex items-center justify-center rounded-full font-semibold`}
      style={{ backgroundColor: "var(--color-primary-light)", color: "var(--color-primary)" }}
    >
      {initial}
    </div>
  );
}
