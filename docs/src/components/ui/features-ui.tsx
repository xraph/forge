"use client";

import { FC, useMemo, useRef, useState, useEffect } from 'react'
import { Canvas, useFrame, useThree } from '@react-three/fiber'
import { Plane } from '@react-three/drei'
import * as THREE from 'three'
// import { Perf } from 'r3f-perf'

const useIsMobile = () => {
  const [isMobile, setIsMobile] = useState(false)

  useEffect(() => {
    const checkIsMobile = () => {
      const userAgent = navigator.userAgent
      const mobileRegex = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i
      const isTouchDevice = 'ontouchstart' in window || navigator.maxTouchPoints > 0
      const isSmallScreen = window.innerWidth <= 768

      setIsMobile(mobileRegex.test(userAgent) || (isTouchDevice && isSmallScreen))
    }

    checkIsMobile()
    window.addEventListener('resize', checkIsMobile)

    return () => window.removeEventListener('resize', checkIsMobile)
  }, [])

  return isMobile
}
const MAX_STEPS = 128
const PRECISION = 0.0005

type AnimationState = {
  positions: any[]
  rotations: any[]
  baseOffsets: {
    x: number
    y: number
    posSpeed: any
    rotSpeed: any
    posPhase: any
    rotPhase: any
  }[]
}

const createInitialState = (amount: number): AnimationState => ({
  positions: Array.from({ length: amount }, () => new THREE.Vector3(0, 0, 0)),
  rotations: Array.from({ length: amount }, () => new THREE.Vector3(0, 0, 0)),
  baseOffsets: Array.from({ length: amount }, (_, i) => {
    const t = (i / amount) * Math.PI * 2
    return {
      x: Math.cos(t) * 1.75,
      y: Math.sin(t) * 4.5,
      posSpeed: new THREE.Vector3(
        1.0 + Math.random() * 4,
        1.0 + Math.random() * 3.5,
        0.5 + Math.random() * 2.0
      ),
      rotSpeed: new THREE.Vector3(
        0.1 + Math.random() * 1,
        0.1 + Math.random() * 1,
        0.1 + Math.random() * 1
      ),
      posPhase: new THREE.Vector3(
        t + Math.random() * Math.PI * 3.0,
        t * 1.3 + Math.random() * Math.PI * 3.0,
        t * 0.7 + Math.random() * Math.PI * 3.0
      ),
      rotPhase: new THREE.Vector3(
        t * 0.5 + Math.random() * Math.PI * 2.0,
        t * 0.8 + Math.random() * Math.PI * 2.0,
        t * 1.1 + Math.random() * Math.PI * 2.0
      )
    }
  })
})

const GLSL_ROTATE = `
// https://gist.github.com/yiwenl/3f804e80d0930e34a0b33359259b556c
mat4 rotationMatrix(vec3 axis, float angle) {
  axis = normalize(axis);
  float s = sin(angle);
  float c = cos(angle);
  float oc = 1.0 - c;
  
  return mat4(oc * axis.x * axis.x + c,           oc * axis.x * axis.y - axis.z * s,  oc * axis.z * axis.x + axis.y * s,  0.0,
              oc * axis.x * axis.y + axis.z * s,  oc * axis.y * axis.y + c,           oc * axis.y * axis.z - axis.x * s,  0.0,
              oc * axis.z * axis.x - axis.y * s,  oc * axis.y * axis.z + axis.x * s,  oc * axis.z * axis.z + c,           0.0,
              0.0,                                0.0,                                0.0,                                1.0);
}

vec3 rotate(vec3 v, vec3 axis, float angle) {
  mat4 m = rotationMatrix(axis, angle);
  return (m * vec4(v, 1.0)).xyz;
}
`

const GLSL_FRESNEL = `
float fresnel(vec3 eye, vec3 normal) {
  return pow(1.0 + dot(eye, normal), 3.0);
}
`

const GLSL_SDF = `
float sdBox( vec3 p, vec3 b ) {
  vec3 q = abs(p) - b;
  return length(max(q,0.0)) + min(max(q.x,max(q.y,q.z)),0.0);
}
`

const GLSL_OPERATIONS = `
float opUnion( float d1, float d2 ) { return min(d1,d2); }

float opSubtraction( float d1, float d2 ) { return max(-d1,d2); }

float opIntersection( float d1, float d2 ) { return max(d1,d2); }

float opSmoothUnion( float d1, float d2, float k ) {
  float h = clamp( 0.5 + 0.5*(d2-d1)/k, 0.0, 1.0 );
  return mix( d2, d1, h ) - k*h*(1.0-h);
}

float opSmoothSubtraction( float d1, float d2, float k ) {
  float h = clamp( 0.5 - 0.5*(d2+d1)/k, 0.0, 1.0 );
  return mix( d2, -d1, h ) + k*h*(1.0-h);
}

float opSmoothIntersection( float d1, float d2, float k ) {
  float h = clamp( 0.5 - 0.5*(d2-d1)/k, 0.0, 1.0 );
  return mix( d2, d1, h ) + k*h*(1.0-h);
}
`

const vertexShader = `
varying vec2 v_uv;

void main() {
  v_uv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`

const createFragmentShader = (amount: number) => `
uniform float u_time;
uniform float u_aspect;
uniform vec3 u_positions[${amount}];
uniform vec3 u_rotations[${amount}];
varying vec2 v_uv;

const int MaxCount = ${amount};
const float PI = 3.14159265358979;

${GLSL_SDF}
${GLSL_OPERATIONS}
${GLSL_ROTATE}
${GLSL_FRESNEL}

float sdf(vec3 p) {
  vec3 correct = 0.1 * vec3(u_aspect, 1.0, 1.0);

  vec3 tp = p + -u_positions[0] * correct;
  vec3 rp = tp;
  rp = rotate(rp, vec3(1.0, 1.0, 0.0), u_rotations[0].x + u_rotations[0].y);
  float final = sdBox(rp, vec3(0.15)) - 0.03;
  
  for(int i = 1; i < MaxCount; i++) {
    tp = p + -u_positions[i] * correct;
    rp = tp;
    rp = rotate(rp, vec3(1.0, 1.0, 0.0), u_rotations[i].x + u_rotations[i].y);
    float box = sdBox(rp, vec3(0.15)) - 0.03;
    final = opSmoothUnion(final, box, 0.4);
  }

  return final;
}

vec3 calcNormal(in vec3 p) {
  const float h = 0.001;
  return normalize(vec3(
    sdf(p + vec3(h, 0, 0)) - sdf(p - vec3(h, 0, 0)),
    sdf(p + vec3(0, h, 0)) - sdf(p - vec3(0, h, 0)),
    sdf(p + vec3(0, 0, h)) - sdf(p - vec3(0, 0, h))
  ));
}

vec3 getHolographicMaterial(vec3 normal, vec3 viewDir, float time) {
  float fresnel = pow(1.0 - max(dot(normal, viewDir), 0.0), 2.0);
  
  float hue = dot(normal, viewDir) * 3.14159 + time * 0.5;
  
  // Green iridescence only - varying shades of green
  vec3 greenShades = vec3(
    0.0,  // No red
    sin(hue) * 0.3 + 0.7,  // Green component with variation
    sin(hue + 1.0) * 0.2 + 0.3   // Slight blue for depth
  );
  
  return greenShades * fresnel * 1.2;
}

vec3 getIridescence(vec3 normal, vec3 viewDir, float time) {
  return getHolographicMaterial(normal, viewDir, time);
}

// Transparent background
vec3 getBackground(vec2 uv) {
  return vec3(0.0); // Fully transparent
}

void main() {
  vec2 centeredUV = (v_uv - 0.5) * vec2(u_aspect, 1.0);
  vec3 ray = normalize(vec3(centeredUV, -1.0));
  
  vec3 camPos = vec3(0.0, 0.0, 2.3);

  vec3 rayPos = camPos;
  float totalDist = 0.0;
  float tMax = 5.0;

  for(int i = 0; i < ${MAX_STEPS}; i++) {
    float dist = sdf(rayPos);

    if (dist < ${PRECISION} || tMax < totalDist) break;

    totalDist += dist;
    rayPos = camPos + totalDist * ray;
  }

  // Start with transparent background
  vec3 color = vec3(0.0);
  float alpha = 0.0;

  if(totalDist < tMax) {
    vec3 normal = calcNormal(rayPos);
    vec3 viewDir = normalize(camPos - rayPos);
    
    vec3 lightDir = normalize(vec3(-0.5, 0.8, 0.6));
    
    float diff = max(dot(normal, lightDir), 0.0);
    
    vec3 halfDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(normal, halfDir), 0.0), 32.0); // Niższy exponent
    
    // Get iridescent base color
    vec3 iridescent = getIridescence(normal, viewDir, u_time);
    
    // Rim lighting for edge glow
    float rimLight = pow(1.0 - max(dot(normal, viewDir), 0.0), 3.0);
    vec3 rimColor = vec3(0.4, 0.8, 1.0) * rimLight * 0.5;
    
    // Ambient occlusion simulation
    float ao = 1.0 - smoothstep(0.0, 0.3, totalDist / tMax);
    
    vec3 baseColor = vec3(0.1, 0.12, 0.15); // Dark base
    color = baseColor * (0.1 + diff * 0.4) * ao;
    color += iridescent * (0.8 + diff * 0.2);
    color += vec3(1.0, 0.9, 0.8) * spec * 0.6;
    color += rimColor;
    
    // Atmospheric perspective
    float fog = 1.0 - exp(-totalDist * 0.2);
    vec3 fogColor = getBackground(centeredUV) * 0.3;
    color = mix(color, fogColor, fog);

    // Make geometry opaque
    alpha = 1.0;
  }

  gl_FragColor = vec4(color, alpha);
}`

interface ScreenPlaneProps {
  animationState: AnimationState
  amount: number
}

const ScreenPlane: FC<ScreenPlaneProps> = ({ animationState, amount }) => {
  const { viewport } = useThree()
  const materialRef = useRef<any>(null!)

  const uniforms = useMemo(() => ({
    u_time: { value: 0 },
    u_aspect: { value: viewport.width / viewport.height },
    u_positions: { value: animationState.positions },
    u_rotations: { value: animationState.rotations },
  }), [viewport.width, viewport.height, animationState.positions, animationState.rotations])

  useFrame((_, delta) => {
    if (materialRef.current) {
      materialRef.current.uniforms.u_time.value += delta
      const time = materialRef.current.uniforms.u_time.value

      animationState.baseOffsets.forEach((offset, i) => {
        const wanderX = Math.sin(time * offset.posSpeed.x + offset.posPhase.x) * 0.8
        const wanderY = Math.cos(time * offset.posSpeed.y + offset.posPhase.y) * 5
        const wanderZ = Math.sin(time * offset.posSpeed.z + offset.posPhase.z) * 0.5

        const secondaryX = Math.cos(time * offset.posSpeed.x * 0.7 + offset.posPhase.x * 1.3) * 0.4
        const secondaryY = Math.sin(time * offset.posSpeed.y * 0.8 + offset.posPhase.y * 1.1) * 0.3

        animationState.positions[i].set(
          offset.x + wanderX + secondaryX,
          offset.y + wanderY + secondaryY,
          wanderZ
        )

        animationState.rotations[i].set(
          time * offset.rotSpeed.x + offset.rotPhase.x,
          time * offset.rotSpeed.y + offset.rotPhase.y,
          time * offset.rotSpeed.z + offset.rotPhase.z
        )

        materialRef.current!.uniforms.u_positions.value[i].copy(animationState.positions[i])
        materialRef.current!.uniforms.u_rotations.value[i].copy(animationState.rotations[i])
      })
    }
  })

  return (
    <Plane args={[1, 1]} scale={[viewport.width, viewport.height, 1]}>
      <shaderMaterial
        ref={materialRef}
        uniforms={uniforms}
        vertexShader={vertexShader}
        fragmentShader={createFragmentShader(amount)}
        transparent={true}
      />
    </Plane>
  )
}

interface AnimationControllerProps {
  animationState: AnimationState
}

const AnimationController: FC<AnimationControllerProps> = ({ animationState }) => {
  useEffect(() => {
    animationState.baseOffsets.forEach((offset, i) => {
      animationState.positions[i].set(offset.x, offset.y, 0)
      animationState.rotations[i].set(0, 0, 0)
    })
  }, [])

  return null
}

export const Scene: FC = () => {
  const isMobile = useIsMobile()
  const amount = isMobile ? 3 : 4
  const [animationState] = useState<AnimationState>(() => createInitialState(amount))

  const cameraConfig = useMemo(() => ({
    position: [0, 0, 15] as [number, number, number],
    fov: 50,
    near: 0.1,
    far: 2000,
  }), [])

  return (
    <div
      className='w-full h-full'

    >
      <Canvas
        camera={cameraConfig}
        dpr={1}
        frameloop="always"
        gl={{
          alpha: true,
          antialias: !isMobile,
          powerPreference: "high-performance"
        }}
      >
        <AnimationController animationState={animationState} />
        <ScreenPlane animationState={animationState} amount={amount} />
        {/* <Perf position='top-left' /> */}
      </Canvas>
    </div>
  )
}