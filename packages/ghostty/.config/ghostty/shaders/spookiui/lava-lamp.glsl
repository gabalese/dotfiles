#define SPOOKI_TIME (iTime - iTimeFocus)
const float SPOOKI_VIBRANCY = 1.00;
const float SPOOKI_DIM = 1.0;

// added by SpookiUI: the treat animation only ever plays in the focused window.
// `iFocus` is 1.0 while focused, 0.0 while unfocused. When this window is
// unfocused we hide the effect and either:
//   - dim + desaturate the terminal (SPOOKI_DIM baked in), so the inactive
//     window is set apart from the focused one; or
//   - render the plain, untouched terminal when dimming is off.
vec4 spooki_out(vec3 res, vec4 term) {
    if (iFocus < 0.5) {
        if (SPOOKI_DIM > 0.5) {
            float g = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));  // luminance
            vec3 inactive = mix(term.rgb, vec3(g), 0.55) * 0.55;    // desaturate, dim
            return vec4(inactive, term.a);
        }
        return term;                                               // plain, no effect
    }
    vec3 fx = term.rgb + (res - term.rgb) * SPOOKI_VIBRANCY;    // scale the effect
    return vec4(min(fx, vec3(1.0)), term.a);
}

// SpookiUI treat: Lava Lamp.
// Slow metaball blobs rising and merging like a 70s lava lamp / the old "Bubbles"
// screensaver. Original SpookiUI shader; a soft, dim glow over dark pixels only.

const int BLOBS = 5;

float h(float n) { return fract(sin(n) * 43758.5453); }

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;
    vec2 p = vec2(uv.x * aspect, uv.y);

    float field = 0.0;
    for (int i = 0; i < BLOBS; i++) {
        float fi = float(i);
        float x = (0.15 + 0.7 * h(fi * 3.7)) * aspect;
        x += 0.06 * sin(SPOOKI_TIME * (0.3 + 0.2 * h(fi)) + fi);
        float speed = 0.05 + 0.05 * h(fi * 9.1);
        float y = fract(h(fi * 5.3) + SPOOKI_TIME * speed);       // rise, wrap
        float r = 0.10 + 0.06 * h(fi * 2.1);
        float d = length(p - vec2(x, y));
        field += r * r / (d * d + 0.0007);                  // metaball falloff
    }

    float blob = smoothstep(0.9, 1.8, field);
    float hue = fract(0.03 * SPOOKI_TIME);
    vec3 col = 0.5 + 0.5 * cos(6.2831 * (hue + vec3(0.0, 0.28, 0.55)));

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);
    vec3 res = term.rgb + col * blob * 0.22 * bgmask;
    fragColor = spooki_out(res, term);
}
