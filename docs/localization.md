# WIFI HUNTER AR — Localization Engine & RSSI Physics

## 1. Physical Foundations & Path Loss Model

Indoor radio frequency (RF) propagation at $2.4\text{ GHz}$ and $5\text{ GHz}$ is dominated by free-space path loss, absorption, reflection, and multipath interference.

WIFI HUNTER AR uses the standard **Log-Distance Path Loss Model** to relate Received Signal Strength Indicator (RSSI) to estimated physical distance:

$$\text{RSSI}(d) = A - 10 \cdot n \cdot \log_{10}(d) + X_\sigma$$

Where:
- $\text{RSSI}(d)$: Received signal strength in dBm at distance $d$ (meters).
- $A$: Reference RSSI at 1 meter distance (calibrated default: $-40.0\text{ dBm}$ for typical Wi-Fi routers).
- $n$: Path loss exponent reflecting indoor clutter (typical values: $2.0$ for free space, $2.5 - 3.5$ for indoor office environments with cubicles and drywall, $4.0+$ for concrete / metal obstruction). Default is $n = 2.7$.
- $X_\sigma$: Zero-mean Gaussian random variable modeling shadow fading ($\sigma \approx 3 - 6\text{ dB}$).
- $d$: Euclidean distance between transmitter (AP) and receiver (Hub).

Inverting this formula yields the distance estimate $\hat{d}$:

$$\hat{d} = 10^{\frac{A - \text{RSSI}}{10 \cdot n}}$$

---

## 2. Signal Preprocessing Pipeline

Raw Wi-Fi measurements from client drivers are notoriously noisy. Before attempting trilateration, the engine applies a two-stage filter:

### 2.1 Outlier Filtering via Median Absolute Deviation (MAD)
Over a rolling buffer of observations from each $(h, \text{BSSID})$ pair:
$$\text{MAD} = \text{median}\left(|R_i - \text{median}(R)|\right)$$

Any sample where $|R_i - \text{median}(R)| > 2.5 \cdot \text{MAD}$ is classified as an anomalous burst (e.g. human body shadowing or temporary adapter power throttling) and discarded.

### 2.2 Exponential Moving Average (EMA) Smoothing
To smooth temporal high-frequency noise without lagging behind slow drift:
$$\bar{R}_t = \alpha \cdot R_t + (1 - \alpha) \cdot \bar{R}_{t-1}$$
Configured with default smoothing coefficient $\alpha = 0.25$.

---

## 3. Multi-Hub 3D Localization Algorithm

Traditional analytical trilateration (e.g., matrix inversion of intersecting spheres) is unstable in the presence of RF noise because spheres rarely intersect at a single point, resulting in complex imaginary solutions or extreme sensitivity to outliers.

Instead, WIFI HUNTER AR implements a **Weighted Non-Linear Least Squares (WNLS) Grid Optimization**:

```
                       ┌────────────────────────┐
                       │  3+ Hub Observations   │
                       └───────────┬────────────┘
                                   │
                                   ▼
                       ┌────────────────────────┐
                       │   Spatial Diversity    │
                       │   Collinearity Check   │
                       └───────────┬────────────┘
                                   │ Pass
                                   ▼
                       ┌────────────────────────┐
                       │ Coarse 3D Grid Search  │
                       │  around Hub Centroid   │
                       │   (step = 1.0 meter)   │
                       └───────────┬────────────┘
                                   │ Minimum Cost
                                   ▼
                       ┌────────────────────────┐
                       │  Fine 3D Grid Search   │
                       │   (step = 0.2 meters)  │
                       └───────────┬────────────┘
                                   │
                                   ▼
                       ┌────────────────────────┐
                       │ Confidence & Error Rad │
                       │    Calculation         │
                       └───────────┬────────────┘
                                   │
                                   ▼
                       ┌────────────────────────┐
                       │ Temporal EMA Smoothing │
                       │  (x, y, z update)      │
                       └────────────────────────┘
```

### 3.1 Observation Aggregation
For each candidate BSSID, the engine gathers smoothed RSSI measurements $\bar{R}_i$ from all distinct hubs $i \in \{1, \dots, H\}$ recorded within a sliding window of $\Delta t \le 45\text{ seconds}$.

A minimum of $H \ge 3$ distinct hubs is mandatory. If $H < 3$, the AP's status is labeled `insufficient_hubs`.

### 3.2 Objective Function
The cost function to minimize is the weighted residual error between candidate position $\mathbf{p} = (x, y, z)$ and estimated distances $\hat{d}_i$:

$$\mathcal{J}(x, y, z) = \sum_{i=1}^{H} w_i \cdot \left( \|\mathbf{p} - \mathbf{h}_i\| - \hat{d}_i \right)^2$$

Where:
- $\mathbf{h}_i = (x_i, y_i, z_i)$ is the known 3D coordinate of Hub $i$.
- $\hat{d}_i$ is the log-distance estimate from Hub $i$.
- $w_i = \frac{1}{\hat{d}_i^2}$ is a distance-inverse weighting factor. Stronger signals (closer hubs) carry substantially higher geometric weight than distant, faded signals.

### 3.3 Two-Stage Search
1. **Coarse Search**: Evaluates candidate coordinates within the bounding box of observing hubs (expanded by $10\text{ meters}$) at $1.0\text{m}$ resolution.
2. **Fine Search**: Refines candidate positions in a $2.0\text{m}$ neighborhood around the coarse minimum at $0.2\text{m}$ resolution.

---

## 4. Confidence & Uncertainty Quantification

Each localized AP is assigned a normalized confidence score $C \in [0.0, 1.0]$ computed as a weighted composite:

$$C = 0.30 \cdot S_{\text{hubs}} + 0.25 \cdot S_{\text{spatial}} + 0.20 \cdot S_{\text{rssi}} + 0.15 \cdot S_{\text{samples}} + 0.10 \cdot S_{\text{residual}}$$

### Component Subscores
1. **Hub Count ($S_{\text{hubs}}$)**:
   $$S_{\text{hubs}} = \min\left(1.0, \frac{H - 3}{3}\right) \times 0.5 + 0.5 \quad (H \ge 3)$$
2. **Spatial Diversity ($S_{\text{spatial}}$)**:
   Measures the angular spread of observing hubs relative to the AP candidate. Hubs arranged along a straight corridor or clustered in one corner receive low scores ($< 0.4$), penalizing ill-conditioned Dilution of Precision (DOP).
3. **RSSI Consistency ($S_{\text{rssi}}$)**:
   Evaluates standard deviation $\sigma_{\text{rssi}}$ of observations. Lower variance increases confidence.
4. **Sample Count ($S_{\text{samples}}$)**:
   Reward for accumulated observation history ($N \ge 30$).
5. **Residual Error ($S_{\text{residual}}$)**:
   Penalizes high cost $\mathcal{J}_{\min}$. If $\mathcal{J}_{\min} < 1.0\text{ m}^2$, $S_{\text{residual}} = 1.0$.

### Error Radius
The estimated 95% confidence radius $r_{\text{err}}$ (in meters) is derived from the residual RMS error:
$$r_{\text{err}} = \max\left(0.5, \sqrt{\frac{\mathcal{J}_{\min}}{H}} \times 1.96\right)$$

### Quality Classification
- **High**: $C \ge 0.75$ and $r_{\text{err}} \le 2.5\text{ m}$
- **Medium**: $0.45 \le C < 0.75$ and $r_{\text{err}} \le 6.0\text{ m}$
- **Low**: $C < 0.45$ or $r_{\text{err}} > 6.0\text{ m}$

---

## 5. Physical Realities & Indoor Limitations

When deploying WIFI HUNTER AR in enterprise or residential environments, the following physical realities must be kept in mind:

1. **Multipath Reflection & Constructive/Destructive Interference**:
   RF signals reflect off concrete walls, metallic framing, windows, and floors. At 2.4/5 GHz, half-wavelengths are $6.2\text{ cm}$ and $2.8\text{ cm}$ respectively. Moving a laptop hub by merely $5\text{ cm}$ can swing observed RSSI by $6-10\text{ dB}$. The EMA and sliding window smoothing mitigate these peaks.
2. **Non-Line-of-Sight (NLOS) Attenuation**:
   A single drywall partition attenuates $5\text{ GHz}$ signals by $\sim 3 - 5\text{ dB}$. A reinforced concrete wall attenuates by $12 - 20\text{ dB}$. Under NLOS conditions, the log-distance equation overestimates true distance. The inverse-distance weighting $w_i = 1/\hat{d}_i^2$ ensures that the closest line-of-sight hubs dominate the solution.
3. **Antenna Polarization & Radiation Patterns**:
   Wi-Fi access point antennas are typically dipole or directional patch antennas with toroidal radiation patterns, not isotropic spheres. Hub orientation can affect received power by $\pm 3\text{ dB}$.
4. **Z-Axis (Vertical) Ambiguity**:
   When all hub laptops sit on standard office desks ($z \approx 0.75 - 1.0\text{ m}$), vertical dilution of precision ($VDOP$) is high. Access points mounted on ceilings ($z \approx 2.5 - 3.2\text{ m}$) may have greater vertical error than horizontal error unless hubs are placed at varying elevations (e.g. shelves or multi-floor spaces).
