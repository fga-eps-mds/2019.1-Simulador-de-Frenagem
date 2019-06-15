import { labelCommand } from "./Command";
import { labelForce } from "./Force";
import { labelRelation } from "./Relation";
import { labelSpeed } from "./Speed";
import { labelTemperature } from "./Temperature";
import { labelVibration } from "./Vibration";

export const empty = 0;
const borderRadius = 1.5;

export const styles = theme => ({
  title: {
    padding: "5px"
  },
  grid: {
    padding: "5px"
  },
  input_file_name: {
    marginLeft: 8,
    flex: 1
  },
  input: {
    display: "none"
  },
  rootUploadFile: {
    padding: "2px 4px",
    display: "flex",
    alignItems: "center",
    width: 400
  },
  formControl: {
    margin: theme.spacing.unit,
    minWidth: 200
  },
  root: {
    flexGrow: 1,
    width: "100%",
    marginTop: "90px"
  },
  appBar: {
    borderRadius: theme.spacing.unit * borderRadius
  }
});

export const sendMessageFunction = (sendMessage, message, variante) => {
  sendMessage({
    message,
    variante,
    condition: true
  });
};

export const calibrationJSON = [
  "calibrationtemperatureSet",
  "calibrationtemperatureSet",
  "calibrationforceSet",
  "calibrationforceSet",
  "speed",
  "vibration",
  "command",
  "relations"
];

export const createQuery = () => {
  const vibration =
    "vibration{acquisitionChanel, conversionFactor, vibrationOffset}";
  const speed = "speed{acquisitionChanel, tireRadius}";
  const relations =
    "relations{transversalSelectionWidth, heigthWidthRelation, rimDiameter, syncMotorRodation, sheaveMoveDiameter, sheaveMotorDiameter}";
  const command =
    "command {commandChanelSpeed ,actualSpeed ,maxSpeed ,chanelCommandPression, actualPression, maxPression}";
  const temperature =
    "calibrationtemperatureSet{acquisitionChanel ,conversionFactor ,temperatureOffset}";
  const force =
    "calibrationforceSet{acquisitionChanel ,conversionFactor ,forceOffset}";
  const query = `${vibration},${speed},${relations},${command},${temperature},${force}, isDefault, name`;
  return query;
};

export const variablesTemperatureOne = [
  { front: "CHT1", back: "acquisitionChanel" },
  { front: "FCT1", back: "conversionFactor" },
  { front: "OFT1", back: "temperatureOffset" }
];

export const variablesTemperatureTwo = [
  { front: "CHT2", back: "acquisitionChanel" },
  { front: "FCT2", back: "conversionFactor" },
  { front: "OFT2", back: "temperatureOffset" }
];

export const variablesForceOne = [
  { front: "CHF1", back: "acquisitionChanel" },
  { front: "FCF1", back: "conversionFactor" },
  { front: "OFF1", back: "forceOffset" }
];

export const variablesForceTwo = [
  { front: "CHF2", back: "acquisitionChanel" },
  { front: "FCF2", back: "conversionFactor" },
  { front: "OFF2", back: "forceOffset" }
];

export const variablesSpeed = [
  { front: "CHR1", back: "acquisitionChanel" },
  { front: "RAP", back: "tireRadius" }
];

export const variablesVibration = [
  { front: "CHVB", back: "acquisitionChanel" },
  { front: "FCVB", back: "conversionFactor" },
  { front: "OFVB", back: "vibrationOffset" }
];

export const variablesCommand = [
  { front: "CHVC", back: "commandChanelSpeed" },
  { front: "CUVC", back: "actualSpeed" },
  { front: "MAVC", back: "maxSpeed" },
  { front: "CHPC", back: "chanelCommandPression" },
  { front: "CUPC", back: "actualPression" },
  { front: "MAPC", back: "maxPression" }
];

export const variablesRelations = [
  { front: "LST", back: "transversalSelectionWidth" },
  { front: "RAL", back: "heigthWidthRelation" },
  { front: "DIA", back: "rimDiameter" },
  { front: "RSM", back: "syncMotorRodation" },
  { front: "DPO", back: "sheaveMotorDiameter" },
  { front: "DPM", back: "sheaveMoveDiameter" }
];

export const allVariablesCalib = [
  variablesTemperatureOne,
  variablesTemperatureTwo,
  variablesForceOne,
  variablesForceTwo,
  variablesSpeed,
  variablesVibration,
  variablesCommand,
  variablesRelations
];

export const createAllCalibrations = [
  {
    mutation: "createTemperature",
    response: "temperature",
    variablesResponse: "id",
    name: "firstTemperature"
  },
  {
    mutation: "createTemperature",
    response: "temperature",
    variablesResponse: "id",
    name: "secondTemperature"
  },
  {
    mutation: "createForce",
    response: "force",
    variablesResponse: "id",
    name: "firstForce"
  },
  {
    mutation: "createForce",
    response: "force",
    variablesResponse: "id",
    name: "secondForce"
  },
  {
    mutation: "createSpeed",
    response: "speed",
    variablesResponse: "id",
    name: "speed"
  },
  {
    mutation: "createVibration",
    response: "vibration",
    variablesResponse: "id",
    name: "vibration"
  },
  {
    mutation: "createCommand",
    response: "command",
    variablesResponse: "id",
    name: "command"
  },
  {
    mutation: "createRelations",
    response: "relations",
    variablesResponse: "id",
    name: "relations"
  }
];

export const variablesCalib = [
  { front: "name", back: "name" },
  { front: "firstTemperature", back: "idFirstTemperature" },
  { front: "secondTemperature", back: "idSecondTemperature" },
  { front: "firstForce", back: "idFirstForce" },
  { front: "secondForce", back: "idSecondForce" },
  { front: "speed", back: "idSpeed" },
  { front: "vibration", back: "idVibration" },
  { front: "command", back: "idCommand" },
  { front: "relations", back: "idRelations" }
];

export const createCalibration = {
  mutation: "createCalibration",
  response: "calibration",
  variablesResponse: "id"
};

export const createDefaultCalibration = {
  mutation: "createDefaultCalibration",
  response: "calibration",
  variablesResponse: "id"
};

export const labels = name => {
  if (labelCommand(name) !== "") return labelCommand(name);
  if (labelForce(name) !== "") return labelForce(name);
  if (labelRelation(name) !== "") return labelRelation(name);
  if (labelSpeed(name) !== "") return labelSpeed(name);
  if (labelTemperature(name) !== "") return labelTemperature(name);
  if (labelVibration(name) !== "") return labelVibration(name);
  return "";
};