import { Toaster as Sonner, type ToasterProps } from "sonner";
import { useTheme } from "@/lib/theme";

export function Toaster(props: ToasterProps) {
  const { resolved } = useTheme();
  return (
    <Sonner
      theme={resolved}
      richColors
      closeButton
      position="bottom-right"
      offset={{ bottom: 56, right: 16 }}
      mobileOffset={{ bottom: 56 }}
      duration={4500}
      visibleToasts={5}
      toastOptions={{ className: "font-sans text-[13px]" }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "10px",
        } as React.CSSProperties
      }
      {...props}
    />
  );
}
