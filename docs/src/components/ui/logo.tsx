

interface ForgeLogoProps {
  size?: number;
  color?: string;
  className?: string;
  animated?: boolean;
}

const svgPaths = {
    p291cf100: "M127 125H0.342773L127 0.137714V125Z",
    p36e5eac0: "M432 342.304V218H558.551L432 342.304Z",
    p417cd40: "M0 551.304V136H127V427L0 551.304Z",
}
;
export function ForgeLogo({ size = 25, color = 'black', className = '', animated = false }: ForgeLogoProps) {
  const aspectRatio = 559 / 552;
  const width = size * aspectRatio;
  const height = size;
  const uniqueId = Math.random().toString(36).substr(2, 9);

  return (
    <div 
      className={className}
      style={{ 
        width: `${width}px`, 
        height: `${height}px`,
        '--fill-0': animated ? `url(#gradientShine-${uniqueId})` : color 
      } as React.CSSProperties}
    >
      <svg className="block size-full" fill="none" preserveAspectRatio="none" viewBox="0 0 559 552">
        <defs>
          <clipPath id="clip0_1_94">
            <rect fill="white" height="551.304" width="559" />
          </clipPath>
          {animated && (
            <linearGradient id={`gradientShine-${uniqueId}`} x1="0%" y1="100%" x2="0%" y2="0%">
              <stop offset="0%" stopColor={color}>
                <animate
                  attributeName="offset"
                  values="-2;1"
                  dur="2.5s"
                  repeatCount="indefinite"
                />
              </stop>
              <stop offset="30%" stopColor={color} stopOpacity="0.7">
                <animate
                  attributeName="offset"
                  values="-1.5;1.3"
                  dur="2.5s"
                  repeatCount="indefinite"
                />
              </stop>
              <stop offset="50%" stopColor="white">
                <animate
                  attributeName="offset"
                  values="-1;1.5"
                  dur="2.5s"
                  repeatCount="indefinite"
                />
              </stop>
              <stop offset="70%" stopColor={color} stopOpacity="0.7">
                <animate
                  attributeName="offset"
                  values="-0.5;1.7"
                  dur="2.5s"
                  repeatCount="indefinite"
                />
              </stop>
              <stop offset="100%" stopColor={color}>
                <animate
                  attributeName="offset"
                  values="0;2"
                  dur="2.5s"
                  repeatCount="indefinite"
                />
              </stop>
            </linearGradient>
          )}
        </defs>
        <g clipPath="url(#clip0_1_94)" id="Group 1">
          <rect fill="var(--fill-0, black)" height="425" id="Rectangle 2" transform="rotate(90 559 1.85773e-05)" width="125" x="559" y="1.85773e-05" />
          <rect fill="var(--fill-0, black)" height="291" id="Rectangle 3" transform="rotate(90 425 218)" width="125" x="425" y="218" />
          <path d={svgPaths.p36e5eac0} fill="var(--fill-0, black)" id="Subtract" />
          <path d={svgPaths.p417cd40} fill="var(--fill-0, black)" id="Union" />
          <path d={svgPaths.p291cf100} fill="var(--fill-0, black)" id="Subtract_2" />
        </g>
      </svg>
    </div>
  );
}
