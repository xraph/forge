import { cn } from "@/lib/utils";

interface MethodBadgeProps {
  method: string;
  size?: "sm" | "md";
}

export const MethodBadge = ({ method, size = "md" }: MethodBadgeProps) => {
  const getMethodColor = (method: string) => {
    switch (method) {
      case "INIT": 
      case "GET": 
        return "bg-blue-500/10 text-blue-600 border-blue-500/20";
      case "REGISTER":
      case "POST": 
        return "bg-green-500/10 text-green-600 border-green-500/20";
      case "SERVICE": 
        return "bg-purple-500/10 text-purple-600 border-purple-500/20";
      case "ROUTE": 
        return "bg-orange-500/10 text-orange-600 border-orange-500/20";
      case "EXTENSION": 
        return "bg-pink-500/10 text-pink-600 border-pink-500/20";
      case "CLI": 
        return "bg-cyan-500/10 text-cyan-600 border-cyan-500/20";
      case "DELETE": 
        return "bg-red-500/10 text-red-600 border-red-500/20";
      default: 
        return "bg-gray-500/10 text-gray-600 border-gray-500/20";
    }
  };

  return (
    <span 
      className={cn(
        "rounded font-semibold border",
        getMethodColor(method),
        size === "sm" ? "px-2 py-1 text-xs" : "px-3 py-1 text-sm"
      )}
    >
      {method}
    </span>
  );
};
